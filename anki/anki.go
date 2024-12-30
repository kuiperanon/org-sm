package anki

import "middleware/emacs"
import "middleware/elemInfo"

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"fmt"
	"time"
	"encoding/json"
	"net/http"
	"bytes"
	"database/sql"
	_ "log"
)

/*
https://docs.ankiweb.net/searching.html
https://foosoft.net/projects/anki-connect/
*/

const (
    AnkiStateNotReview = 0
    AnkiStateInReview = 1
)

const (
	maxRetries = 8
    ankiConnectServerPort = 8765
    ankiConnectApiVersion = 6
    baseAnkiQuery = "deck:" + MainDeck + " tag:org-sm -tag:org-sm-skip"
    // Exported
    MainDeck = "org-sm"
)

var MainDeckId int
var ankiState int

var UuidCache     map[int]string // Maps Card ID to UUID
var NoteIdCache   map[int]int    // Maps Card ID to Note ID
var CardIdCache   map[string]int // Maps UUID to Card ID

type UuidCacheDb struct {
    db                  *sql.DB
}


var UuidCacheDbPath string

var uuidCacheDb UuidCacheDb

var uuidCacheDbInitialized bool

// TODO nocheckin :: Make it so that all DELETES to anki must first do it to the Cache. Only read from Anki when it's missing from the Cache. We must assume that the Cache matches Anki for the UpdateAndSync stuff. We can confirm that the Cache contains all the CardIds from Anki, and if it doesn't then we can find the missing ones and add them to the Cache--this can be done during UpdateAndSync at the start of it.
// nocheckin :: Call it AnkiCacheDb instead of UuidCacheDb

// nocheckin just for debugging
func DebugUuidCacheDb() bool {
	return uuidCacheDb.has(1718483581717)
}

func (u *UuidCacheDb) Init() error {
    // Now read the database
	homeDir, err := os.UserHomeDir()
	if err != nil {
        return err
	}

	// TODO nocheckin :: Delete this database, if it turns out its' fast enough to do without
	UuidCacheDbPath = filepath.Join(homeDir, ".local", "share", "org-sm", "anki.db")
	if err := os.MkdirAll(filepath.Dir(UuidCacheDbPath), os.ModePerm); err != nil {
        return err
    }

    // Create and connect to the SQLite database
    db, err := sql.Open("sqlite3", UuidCacheDbPath)
    if err != nil {
        return err
    }

    // Check the connection
    if err := db.Ping(); err != nil {
        return err
    }

    // Create the Element table if it doesn't exist
    createTableSQL := `
    CREATE TABLE IF NOT EXISTS UuidCache (
        cardId INTEGER PRIMARY KEY,
        uuid TEXT
    );`

    if _, err = db.Exec(createTableSQL); err != nil {
        return err
    }

    u.db = db

    return nil
}

func (u *UuidCacheDb) put(cardId int, uuid string) error {
    // Insert or update the cardId and uuid into the database
    insertSQL := `
    INSERT INTO UuidCache (cardId, uuid)
    VALUES (?, ?)
    ON CONFLICT(cardId) DO UPDATE SET uuid = excluded.uuid;`

    _, err := u.db.Exec(insertSQL, cardId, uuid)
    return err
}

func (u *UuidCacheDb) PopulateUuidCache() error {
	rows, err := u.db.Query("SELECT cardId, uuid FROM UuidCache;")
	if err != nil {
		log.Fatal(err)
	}

	// Iterate through the rows
	for rows.Next() {
		var cardId int
		var uuid string

		err = rows.Scan(&cardId, &uuid)
		if err != nil {
			return err
		}

		// Fill the map
		UuidCache[cardId] = uuid
		CardIdCache[uuid] = cardId
	}
	return nil
}


func (u *UuidCacheDb) has(cardId int) bool {
    // Check if the cardId exists in the database
    querySQL := `SELECT 1 FROM UuidCache WHERE cardId = ? LIMIT 1;`

    var exists int
    err := u.db.QueryRow(querySQL, cardId).Scan(&exists)
    return err == nil && exists == 1
}

type Review struct {
    ReviewTime       int         `json:"id"`
    Usn              interface{} `json:"usn"`
    ButtonPressed    interface{} `json:"ease"`
    NewInterval      int         `json:"ivl"`
    PreviousInterval int         `json:"lastIvl"`
    NewFactor        interface{} `json:"factor"`
    ReviewDuration   interface{} `json:"time"`
    ReviewType       interface{} `json:"type"`
}

func (r *Review) GetReviewTime() time.Time {
	// TODO nocheckin :: Does this need to be used ever?
    if r.ReviewTime == 0 {
        return time.Now()
    }
    return time.Unix(int64(r.ReviewTime / 1000), 0)
}

func GetReviewTimeNow() int {
	return int(int64(time.Now().Unix()) * 1000)
}

type Response struct {
    Result interface{} `json:"result"`
    Error  string      `json:"error"`
}

// TODO perhaps may want to add the "content" as well
func CreateCard(uuid string, content string) error {
	_ = content // TODO implement content. :nocheckin
    resp, err := Request("addNote", map[string]interface{}{
        "note": map[string]interface{}{
            "deckName": MainDeck,
            "modelName": "Basic",
            "fields": map[string]interface{}{
                "Front": uuid,
                "Back": uuid,
            },
            "options": map[string]interface{}{
                "allowDuplicate": false,
                "duplicateScope": "deck",
                "duplicateScopeOptions": map[string]interface{}{
                    "deckName": MainDeck,
                    "checkChildren": false,
                    "checkAllModels": false,
                },
            },
            "tags": []string{"org-sm", "uuid-" + uuid},
        },
    })
    if err != nil {
        fmt.Printf("error in sending addNote request: %s\n", uuid)
        return err
    }
	_ = resp

	// Also insert cardId into the database
	cardId, cardExists, err := FindCard(uuid)
	if err != nil {
		err = fmt.Errorf("Error from anki.FindCard: %v", err)
		return err
	}
	if !cardExists {
		err = fmt.Errorf("CreateCard: Created card but it doesn't exist right creating it. uuid = %s", uuid)
		return err // No need to delete if card doesn't exist.
	}
	UuidCache[cardId] = uuid
	err = uuidCacheDb.put(cardId, uuid)
	if err != nil {
		return err
	}

    return nil
}

func Request(action string, params map[string]interface{}) (*Response, error) {
	payload := map[string]interface{}{
		"action":  action,
		"params":  params,
		"version": ankiConnectApiVersion,
	}

	// Marshal the payload to JSON
    jsonData, err := json.Marshal(payload)
    //fmt.Printf("nocheckin JSON: %s\n", jsonData)
	if err != nil {
		fmt.Printf("Error marshalling JSON: %v\n", err)
		return nil, err
	}

	// Create the request
	url := fmt.Sprintf("http://localhost:%d", ankiConnectServerPort)

	var resp *http.Response
	for attempt := 1; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			fmt.Printf("Error creating request: %v\n", err)
			return nil, err
		}

		// Set the Content-Type header
		req.Header.Set("Content-Type", "application/json")

		// Send the request
		client := &http.Client{}
		resp, err = client.Do(req)
		if err != nil {
			fmt.Printf("Error sending request (attempt %d/%d): %v\n", attempt, maxRetries, err)
			if attempt < maxRetries {
				time.Sleep(time.Second)
				continue
			}
			return nil, err
		}
		defer resp.Body.Close()

		// Check the response
		if resp.StatusCode != http.StatusOK {
			if attempt < maxRetries {
				time.Sleep(time.Second)
				continue
			}
			return nil, fmt.Errorf("request failed with status code: %d", resp.StatusCode)
		}
	}

    data, err := io.ReadAll(resp.Body)
    if err != nil {
		fmt.Printf("error making reading http response: %s\n", err)
		return nil, err
    }

    var response Response
    err = json.Unmarshal(data, &response)
    if err != nil {
		fmt.Printf("error unmarshalling json respond: %s\n", err)
        return nil, err
    }
    return &response, nil
}

func InReview() bool {
	return ankiState == AnkiStateInReview
}

func StartReview() error {
    if InReview() {
		// Already in review.
        return nil
    }

    resp, err := Request("guiDeckReview", map[string]interface{}{
        "name": MainDeck,
    })
    if err != nil {
        err = fmt.Errorf("error in sending guiDeckReview request: %s", err.Error())
        return err
    }
	success := resp.Result.(bool)
	if !success {
		err = fmt.Errorf("error with guiDeckReview, result was not 'true'.")
		return err
	}
	ankiState = AnkiStateInReview
    return nil
}

func VerifyConnectionAndDb() error {
	// Confirm the database exists for the anki UUID cache
	if !uuidCacheDbInitialized {
		if err := uuidCacheDb.Init(); err != nil {
			return fmt.Errorf("Anki note id to UUID cache Database failed to reinit with error: %v\n", err)
		} else {
			uuidCacheDbInitialized = true
			fmt.Println("Anki note id to UUID cache Database Reinitialized database succesfully.")
		}
	}

	// Verify connection
    resp, err := Request("deckNamesAndIds", map[string]interface{}{})
    if err != nil {
		err = fmt.Errorf("Error sending `deckNamesAndIds` Anki.Request: %v", err)
        return err
    }

    const NOT_FOUND = -1
    MainDeckId := NOT_FOUND

    for name, idF := range resp.Result.(map[string]interface {}) {
        _id := int(idF.(float64))
        if name == MainDeck {
            MainDeckId = _id
            break
        }
    }

    if MainDeckId == NOT_FOUND {
        // Create the deck, since it's missing.
        resp, err := Request("createDeck", map[string]interface{}{"deck": MainDeck})
        if err != nil {
            err = fmt.Errorf("error in sending createDeck request: %s", err.Error())
            return err
        }
        fmt.Printf("Deck is missing, so created deck, response to createDeck request is: %s\n", resp.Result)
    }

	return nil
}

func DeleteElemInfo(ei elemInfo.ElemInfo) error {
	return DeleteCardWithUuid(ei.Uuid)
}

func DeleteCardWithUuid(uuid string) error {
	cardId, cardExists, err := FindCard(uuid)
	if err != nil {
		err = fmt.Errorf("Error from anki.FindCard: %v", err)
		return err
	}
	if !cardExists {
		return nil // No need to delete if card doesn't exist.
	}
	return DeleteCard(cardId)
}

func DeleteCard(cardId int) error {
	return DeleteCards([]int{cardId})
}

func CardsToNotes(cardIds []int) (noteIds []int, err error) {
	var uncachedCardIds []int
	for _, cardId := range cardIds {
		noteId, exists := NoteIdCache[cardId]
		if exists {
			noteIds = append(noteIds, noteId)
		} else {
			uncachedCardIds = append(uncachedCardIds, cardId)
		}
	}
	// Retrieve uncached cardIds from Anki to add to returned noteIds and cache
	if len(uncachedCardIds) > 0 {
		resp, err := Request("cardsToNotes", map[string]interface{}{"cards": uncachedCardIds})
		if err != nil {
			fmt.Printf("error in sending `cardsToNotes` request\n")
			return noteIds, err
		}
		for idx, noteId_ := range resp.Result.([]interface{}) {
			noteId := int(noteId_.(float64))
			NoteIdCache[uncachedCardIds[idx]] = noteId
			noteIds = append(noteIds, noteId)
		}
	}
	return
}

func DeleteCards(cardIds []int) error {
	// TODO nocheckin :: Make this remove from the AnkiCacheDb
    if len(cardIds) == 0 {
        return nil
    }

    noteIds, err := CardsToNotes(cardIds)
    if err != nil {
        fmt.Printf("error = %v\n", err)
        return err
    }

	fmt.Println("Deleting notes: ", noteIds)
    resp, err := Request("deleteNotes", map[string]interface{}{
        "notes": noteIds,
    })
    _ = resp
    if err != nil {
        fmt.Printf("error in sending `deleteNotes` request\n")
        return err
    }

    return nil
}

func FindCards() (cardIds []int, err error) {
    resp, err := Request("findCards", map[string]interface{}{"query": baseAnkiQuery})
    if err != nil {
        fmt.Printf("error in sending `findCards` request\n")
        return []int{}, err
    }
    if len(resp.Result.([]interface {})) == 0 {
        return []int{}, nil
    }

    cardIdsInterface := resp.Result.([]interface{})
    cardIds = make([]int, len(cardIdsInterface))
    for i, v := range cardIdsInterface {
        cardIds[i] = int(v.(float64))
    }

    return
}

func FindDueCards() (cardIds []int, err error) {
	// TODO nocheckin ::
    //resp, err := Request("findCards", map[string]interface{}{"query": baseAnkiQuery + " (is:due or is:new)"})
    resp, err := Request("findCards", map[string]interface{}{"query": baseAnkiQuery + " is:due"})
    if err != nil {
        fmt.Printf("error in sending `FindDueCards` request\n")
        return []int{}, err
    }
    if len(resp.Result.([]interface {})) == 0 {
        return []int{}, nil
    }

    cardIdsInterface := resp.Result.([]interface{})
    cardIds = make([]int, len(cardIdsInterface))
    for i, v := range cardIdsInterface {
        cardId := int(v.(float64))
        cardIds[i] = cardId
    }

    return
}

func FindUuidFromCard(cardId int) (uuid string, err error) {
	// TODO nocheckin :: we are assuming for now that cardId == noteId. (I will find out if this will cause problems or not)
	uuid, exists := UuidCache[cardId]
	if exists {
		return uuid, nil
	}

    noteIds, err := CardsToNotes([]int{cardId})
    if err != nil {
        fmt.Println(err)
        return "", err
    }
	noteId := noteIds[0]
	noteTagsResp, err := Request("getNoteTags", map[string]interface{}{"note": noteId})
	if err != nil {
		fmt.Printf("error in sending request: getNoteTags\n")
		return "", err
	}
	//fmt.Println("noteTagsResp = ", noteTagsResp)
	for _, tag := range noteTagsResp.Result.([]interface{}) {
		tag := tag.(string)
		if (strings.HasPrefix(tag, "uuid-")) {
			uuid = tag[len("uuid-"):]
			break
		}
	}
	if len(uuid) != 36 {
		fmt.Printf("error: uuid should be 36 characters. uuid = %s\n", uuid)
		err = fmt.Errorf("error: uuid should be 36 characters. uuid = %s\n", uuid)
		return "", err
	}
	UuidCache[cardId] = uuid
	err = uuidCacheDb.put(cardId, uuid)
	if err != nil {
		return "", err
	}
	return uuid, nil
}

func FindElemInfoWithCardId(cardId int) (ei elemInfo.ElemInfo, elemExists bool, err error) {
	uuid, err := FindUuidFromCard(cardId)
	if err != nil {
		err = fmt.Errorf("error FindElemInfoWithCardId: err: %v\n", err)
		fmt.Println(err)
		return
	}
	ei, elemExists, err = emacs.FindElemInfo(uuid)
	if err != nil {
		err = fmt.Errorf("error FindElemInfoWithCardId: err: %v\n", err)
		fmt.Println(err)
		return
	}
	if !elemExists {
		return ei, false, nil
	}
	return
}

func FindCard(uuid string) (cardId int, exists bool, err error) {
    cachedCardId, exists := CardIdCache[uuid]
    if exists {
        return cachedCardId, true, nil
    } else {
        resp, err := Request("findCards", map[string]interface{}{"query": "deck:" + MainDeck + " tag:org-sm tag:uuid-" + uuid })
        if err != nil {
            err = fmt.Errorf("error in sending `findCards` request for uuid: %s, error: %v", uuid, err)
            return -1, false, err
        }
        if len(resp.Result.([]interface {})) != 1 {
            return -1, false, nil
        }

        cardId := int(resp.Result.([]interface {})[0].(float64))
        CardIdCache[uuid] = cardId
        UuidCache[cardId] = uuid
        return cardId, true, nil
    }
}

func CurrentElemInfo() (ei elemInfo.ElemInfo, deckEmpty bool, err error) {
	// Start review if unstarted
	if err := StartReview(); err != nil {
		err = fmt.Errorf("error in starting review: %v", err)
		return ei, false, err
	}

	resp, err := Request("guiCurrentCard", map[string]interface{}{})
	if err != nil {
		return
	}

	if resp.Result == nil {
		return ei, false, nil
	}
	m := resp.Result.(map[string]interface{})
	noExistErr := fmt.Errorf("Invalid guiCurrentCard response: %v", resp.Result)
    deckName, exists := m["deckName"]
	if !exists {
		return ei, false, noExistErr
	}
	if deckName != MainDeck {
		ankiState = AnkiStateNotReview
		return ei, false, fmt.Errorf("Anki is in review for wrong deck, current deck is %s", deckName)
	}
    cardIdF, exists := m["cardId"]
	cardId := int(cardIdF.(float64))
	if !exists {
		return ei, false, noExistErr
	}

	ei, elemExists, err := FindElemInfoWithCardId(cardId)
	if err != nil {
		return
	}
	if !elemExists {
		//return ei, true, fmt.Errorf("Element does not exist for cardId: %v", cardId)
		deckEmpty = true
		return ei, deckEmpty, nil
	}

	resp, err = Request("guiShowAnswer", map[string]interface{}{})
	if err != nil {
		return
	}

	return
}

func SetGrade(grade int) (ok bool, err error) {
	fmt.Println("SetGrade: ", grade)
	resp, err := Request("guiAnswerCard", map[string]interface{}{"ease": grade})
	if err != nil {
		err = fmt.Errorf("error in sending `guiAnswerCard` request")
		return false, err
	}
	fmt.Println("nocheckin SetGrade: ", resp.Result)
	//if len(resp.Result.([]interface {})) != 1 {
	//    return -1, false, nil
	//}
	return resp.Result.(bool), nil
}

func SyncCacheDbWithAnki() error {
	// TODO nocheckin
	// Populate UuidCache with data from the database
	// Confirm that we match the cardIds in anki


	log.Fatal("Unimplemented")

	return nil
}

func UpdateCaches() error {
    cardIds, err := FindCards()
    if err != nil {
        return err
    }

	// Update NoteIdCache
	_, err = CardsToNotes(cardIds)
	if err != nil {
		return err
	}

	// Update UuidCache
	uuidCacheDb.PopulateUuidCache()

	return nil
}
