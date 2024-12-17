package anki

import "middleware/emacs"

import (
	"io"
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
	maxRetries = 5
    ankiConnectServerPort = 8765
    ankiConnectApiVersion = 6
    baseAnkiQuery = "deck:" + MainDeck + " tag:org-sm -tag:org-sm-skip"
    // Exported
    MainDeck = "org-sm"
)

var MainDeckId int
var ankiState int

var UuidCache     map[int]string // Maps Card ID to UUID
var CardIdCache   map[string]int // Maps UUID to Card ID

type UuidCacheDb struct {
    db                  *sql.DB
}


var UuidCacheDbPath string

var uuidCacheDb UuidCacheDb

var uuidCacheDbInitialized bool

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

func (u *UuidCacheDb) get(cardId int) (string, error) {
    // Retrieve the uuid for the given cardId
    querySQL := `SELECT uuid FROM UuidCache WHERE cardId = ?;`

    var uuid string
    err := u.db.QueryRow(querySQL, cardId).Scan(&uuid)
    if err != nil {
        if err == sql.ErrNoRows {
            return "", nil // No matching record found
        }
        return "", err // Some other error occurred
    }
    return uuid, nil
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
	_ = content // TODO implement content! :nocheckin
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
	_ = resp
    if err != nil {
        fmt.Printf("error in sending addNote request: %s\n", uuid)
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

func DeleteCard(cardId int) error {
	//log.Fatal("anki.DeleteCurrentCard -- haven't tested if this works yet")
	// Get the note id
    resp, err := Request("cardsToNotes", map[string]interface{}{"cards": []int{cardId}})
    if err != nil {
        fmt.Printf("error in sending `cardsToNotes` request\n")
        return err
    }
	fmt.Println("Deleting card: ", resp.Result, ", cardId was=", cardId)

    for _, noteId := range resp.Result.([]interface{}) {
		resp, err := Request("deleteNotes", map[string]interface{}{"notes": []int{int(noteId.(float64))}})
		if err != nil {
			fmt.Printf("error in sending request: deleteNotes\n")
			return err
		}
		fmt.Println("Deleted card: ", cardId, ", resp =", resp.Result)
		break
	}

	return nil
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

func DeleteEntriesWithUuids(uuids []string) error {
    /*
    TODO nocheckin complete this
    For each


maybe I can `or` a bunch of uuid tags
    */

    fmt.Println("TODO call3ed DeleteEntriesFromAnki- I still need to do this function!")
    return nil
}


func DeleteCards(cardIds []int) error {
    if len(cardIds) == 0 {
        return nil
    }
	// TODO : consider what's below, but it may slow things down too much and may not be necessary. Seems to be the case that cardIds are the same as noteIds if there's only the typical number of cards made for the note.

    //var noteIds []int
    //for _, x := range cardIds {
    //
    //}

    resp, err := Request("deleteNotes", map[string]interface{}{
        "notes": cardIds,
    })
    _ = resp
    if err != nil {
        fmt.Printf("error in sending `deleteNotes` request\n")
        return err
    }

    return nil
}

func FindCardIds() (cardIds []int, err error) {
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

func FindDueCards(db *sql.DB) (cardIds []int, err error) {
    resp, err := Request("findCards", map[string]interface{}{"query": baseAnkiQuery + " (is:due or is:new)"})
    if err != nil {
        fmt.Printf("error in sending `FindDueCards` request\n")
        return []int{}, err
    }
    if len(resp.Result.([]interface {})) == 0 {
        return []int{}, nil
    }

    cardIdsInterface := resp.Result.([]interface{})
	MAX_CARDS := 100
    cardIds = make([]int, min(MAX_CARDS, len(cardIdsInterface)))
    //cardIdsMissingInCache := []int{}
    for i, v := range cardIdsInterface {
		if i >= MAX_CARDS {
			break
		}
        cardId := int(v.(float64))
        cardIds[i] = cardId
    }

    return
}

func UpdateCache(cardIds_ []int) error {
	// Filter out the cardIds which are already in the database
	var cardIds []int
	for _, v := range cardIds_ {
		if !uuidCacheDb.has(v) {
			cardIds = append(cardIds, v)
		} else {
			uuid, err := uuidCacheDb.get(v)
			if err != nil {
				return err
			}
			UuidCache[v] = uuid
		}
	}
    if len(cardIds) == 0 {
        return nil
    }

    resp, err := Request("cardsToNotes", map[string]interface{}{"cards": cardIds})
    if err != nil {
        fmt.Printf("error in sending `cardsToNotes` request\n")
        return err
    }
    for i, v := range resp.Result.([]interface{}) {
		noteId := int(v.(float64))
		noteTagsResp, err := Request("getNoteTags", map[string]interface{}{"note": noteId})
		if err != nil {
			fmt.Printf("error in sending request: getNoteTags\n")
			return err
		}
		uuid := ""
		fmt.Println("noteTagsResp = ", noteTagsResp)
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
			return err
		}
		UuidCache[cardIds[i]] = uuid
		if err := uuidCacheDb.put(cardIds[i], uuid); err != nil {
			return fmt.Errorf("Failed to put--anki uuidCacheDb: %s\n", err.Error())
		}

		//_, err = emacs.FindElemInfo(uuid)
		if err != nil {
			return err
		}
		fmt.Println("Caching UUID from anki: noteId: ", noteId, ", response: ", uuid)
	}
	return nil
}

func FindElemInfoWithCardId(cardId int) (elemInfo emacs.ElemInfo, elemExists bool, err error) {
	uuid, exists := UuidCache[cardId]
	if !exists {
        UpdateCache([]int{cardId})
        uuid, exists = UuidCache[cardId]
        if !exists {
            err = fmt.Errorf("error: UUID could not be found for cardId: %d\n", cardId)
            fmt.Println(err)
            return
        }
    }
    elemInfo, elemExists, err = emacs.FindElemInfo(uuid)
	elemInfo.CardId = cardId
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

// TODO nocheckin : Can I delete this function? Does it do anything important?
func CurrentElemInfo() (ei emacs.ElemInfo, deckEmpty bool, err error) {
	// Start review if unstarted
	if err := StartReview(); err != nil {
		err = fmt.Errorf("error in starting review: %v", err)
		return ei, false, err
	}

	resp, err := Request("guiCurrentCard", map[string]interface{}{})
	if err != nil {
		return
	}
	//fmt.Println("nocheckin CurrentElemenInfo: ", resp.Result)

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

	fmt.Println("nocheckin Finding elemInfoWithCardId: cardId= ", cardId)
	ei, elemExists, err := FindElemInfoWithCardId(cardId)
	if err != nil {
		return
	}
	if !elemExists {
		//return ei, true, fmt.Errorf("Element does not exist for cardId: %v", cardId)
		deckEmpty = true
		return ei, deckEmpty, nil
	}

	fmt.Println("callin' guiShowAnswer")
	resp, err = Request("guiShowAnswer", map[string]interface{}{})
	if err != nil {
		return
	}
	fmt.Println("nocheckin", resp.Result)

    //err = json.Unmarshal([]uint8(), &response)
    //if err != nil {
    //    return
    //}
	//fmt.Println("nocheckin : ", response)

	// TODO nocheckin delete this after quick test!
	// deckEmpty = true // nocheckin deckEmpty
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
