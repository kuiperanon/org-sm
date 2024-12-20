package middlewareDb

import "middleware/emacs"
import "middleware/anki"

import (
	"database/sql"
	"os"
	"fmt"
	"sync"
	"time"
	"os/exec"
	"log"
	"sort"
	"strconv"
	"math/rand"
	"strings"
	"path/filepath"
    _ "github.com/mattn/go-sqlite3"
)

type MiddlewareDb struct {
    db                  *sql.DB
}

var learningQueue []emacs.ElemInfo
var learningQueueMutex sync.Mutex

func LearningQueuePopFront() {
	learningQueueMutex.Lock()
	defer learningQueueMutex.Unlock()
	learningQueue = learningQueue[1:]
}

func IsLearningQueueEmpty() bool {
	learningQueueMutex.Lock()
	defer learningQueueMutex.Unlock()
	return len(learningQueue) == 0
}

func LearningQueue() []emacs.ElemInfo {
	learningQueueMutex.Lock()
	defer learningQueueMutex.Unlock()
	return learningQueue
}

func (m *MiddlewareDb) DidVerifyAndCleanToday() (bool, error) {
	var lastSyncDate string

	// Query to fetch last sync date
	row := middlewareDb.db.QueryRow("SELECT last_sync_date FROM State WHERE id = 1")
	err := row.Scan(&lastSyncDate)
	if err != nil {
		if err == sql.ErrNoRows {
			// No entry found
			return false, nil
		}
		return false, err
	}

	date := time.Now().Format("2006-01-02")
	didToday := date == lastSyncDate

	return didToday, nil
}

var middlewareDb MiddlewareDb

var ElementsDbPath string

var HeartBeatRunning bool

func HeartBeat() error {
    if HeartBeatRunning {
        return nil
    }
    HeartBeatRunning = true

	// Confirm the database exists for this software.
	homeDir, err := os.UserHomeDir()
	if err != nil {
        return err
	}
	ElementsDbPath = filepath.Join(homeDir, ".local", "share", "org-sm", "elements.db")
	if _, err := os.Stat(ElementsDbPath); os.IsNotExist(err) {
		if err := Init(); err != nil {
			return fmt.Errorf("Database was missing and then failed to reinit with error: %v\n", err)
		} else {
			fmt.Println("Database was missing, then Reinitialized database succesfully.")
		}
	} else if err != nil {
		return fmt.Errorf("Error checking database at path: %s, error: %v\n", ElementsDbPath, err)
	} else {
		//fmt.Println("Database found.")
	}

	// Verify the connection with emacs exists
    cmd := exec.Command("emacsclient", "-e", "(+ 1 1)")
    stdout, err := cmd.Output()
    if err != nil {
		err = fmt.Errorf("emacsclient ping test failed: %v", err)
        return err
    }
    s := string(stdout)
	if s[0] != '2' {
		return fmt.Errorf("Failed to connect to emacs server")
	}

	// Confirm there's a connection here.
	if err := anki.VerifyConnectionAndDb(); err != nil {
		return err
	}

	if err := anki.StartReview(); err != nil {
		err = fmt.Errorf("error from StartReview: %s", err.Error())
		return err
	}

	// Update the learning queue
	if IsLearningQueueEmpty() {
		err = UpdateLearningQueue()
		if err != nil {
			err = fmt.Errorf("error from UpdateLearningQueue: %s", err.Error())
			log.Fatal(err)
		}
	}

	if err := UpdateAndSyncIfRequired(); err != nil {
		err = fmt.Errorf("error in UpdateAndSyncIfRequired: %v", err)
		return err
	}

    HeartBeatRunning = false
	return nil
}

func UpdateAndSyncIfRequired() error {
	didClean, err := middlewareDb.DidVerifyAndCleanToday()
	if err != nil {
		err = fmt.Errorf("error from DidVerifyAndCleanToday: %v", err)
		return err
	}
	if didClean {
		// Skip since we already updated and synced today
		//fmt.Println("nocheckin we already did a sync")
		return nil
	}

	// TODO nocheckin ; tell anki to turn off the review state, then set the AnkiState off of AnkiStateInReview

    fmt.Println("Verifying and cleaning middleware db. This may take a while.")

    fmt.Println("Finding Uuids in emacs...")
    uuidsEmacs, err := emacs.FindUuidsSorted(); _ = uuidsEmacs
    if err != nil {
        return err
    }

    if err := VerifyAndCleanWithEmacs(uuidsEmacs); err != nil {
		err = fmt.Errorf("error from VerifyAndCleanWithEmacs: %v", err)
        return err
    }
    if err := VerifyAndCleanWithAnki(uuidsEmacs); err != nil {
		err = fmt.Errorf("error from VerifyAndCleanWithAnki: %v", err)
        return err
    }

	// Update the learning queue
	err = UpdateLearningQueue()
	if err != nil {
        err = fmt.Errorf("error from UpdateLearningQueue: %s", err.Error())
        log.Fatal(err)
    }

	if err := UpdateLastVerifyAndCleanDate(); err != nil {
		err = fmt.Errorf("error from UpdateLastVerifyAndCleanDate: %v", err)
		return err
	}

	// Next update the queue

    return nil
}

func UpdateLastVerifyAndCleanDate() error {
	syncDate := time.Now().Format("2006-01-02")

	// Upsert the sync date
	_, err := middlewareDb.db.Exec(`INSERT INTO State (id, last_sync_date) VALUES (1, ?)
					  ON CONFLICT(id) DO UPDATE SET last_sync_date = excluded.last_sync_date`, syncDate)
	if err != nil {
		return err
	}
	return nil
}

// TODO rename this. Because it also does stuff for anki. I can think of better names for these helper functions.
// TODO add DOC COMMENT that explains that the uuidsEmacs argument must be pre-sorted before being passed in
func VerifyAndCleanWithEmacs(uuidsEmacs []string) error {
    // TODO nocheckin : I think here I should handle Anki. Add "org-sm-skip" and "org-sm-flag-corrupt" tags to anki
    // Remove entries from Middlware DB which are not present in any emacs elements.
    fmt.Println("Finding Uuids in MWdb")
    uuidsMwdb, err := FindUuids()
    if err != nil {
		err = fmt.Errorf("error from FindUuids: %v", err)
        return err
    }
    fmt.Println("Sorting Uuids from MWdb")
    sort.Strings(uuidsMwdb)

    fmt.Println("Deleting unneeded Uuids in MWdb and/or Anki")
    var uuidsToDelete []string
    for _, uuid := range uuidsMwdb {
        if sort.SearchStrings(uuidsEmacs, uuid) == len(uuidsEmacs) {
            uuidsToDelete = append(uuidsToDelete, uuid)
        }
    }
    if len(uuidsToDelete) > 0 {
        if err = DeleteEntries(uuidsToDelete, DeleteDefault); err != nil {
			err = fmt.Errorf("error from DeleteEntries: %v", err)
            return err
        }
    }

    fmt.Println("Updating MWDB with Emacs data")
    // Update the Middleware DB with Emacs data
    for _, uuid := range uuidsEmacs {
        elemInfo, elemExists, err := emacs.FindElemInfo(uuid)
		if err != nil {
			err = fmt.Errorf("error from emacs.FindElemInfo: %v", err)
			return err
		}
		if !elemExists {
			continue
		}

        // Emacs uuid missing from MiddlewareDb
        if sort.SearchStrings(uuidsMwdb, uuid) == len(uuidsMwdb) {
            fmt.Println("uuid ", uuid, " missing from mwdb")
            if elemInfo.IsItem() {
                // TODO : Handle case where there already exists an Anki card... Currently it throws an error, because AnkiConnect returns an error message JSON referring to this being an invalid request due to it being a duplicate card
                // Create missing card in Anki
                if err = anki.CreateCard(elemInfo.Uuid, ""/*TODO nocheckin  , elemInfo.content? */); err != nil {
					err = fmt.Errorf("error from anki.CreateCard: %v", err)
                    return err
                }
            }
        }

        // Update elemInfo cardId from Anki
        if elemInfo.IsItem() {
            cardId, cardExists, err := anki.FindCard(uuid)
            elemInfo.CardId = cardId
            if err != nil {
				err = fmt.Errorf("Error from anki.FindCard: %v", err)
				fmt.Println(err)
                return err
            }
            if !cardExists {
                // Try again
                if err = anki.CreateCard(elemInfo.Uuid, ""); err != nil {
                    return err
                }
                cardId, cardExists, err := anki.FindCard(uuid)
                elemInfo.CardId = cardId
                if err != nil {
                    return err
                }
                if !cardExists {
                    return fmt.Errorf("error: Could not find Anki card for uuid = %s\n", elemInfo.Uuid)
                }
            }
        }

        if err := UpdateOrInsertWithElemInfo(elemInfo, false); err != nil {
			err = fmt.Errorf("error from UpdateOrInsertWithElemInfo: %v", err)
			return err
        }
    }

    return nil
}

func VerifyAndCleanWithAnki(uuidsEmacs []string) error {
    cardIds, err := anki.FindCardIds()
    sort.Ints(cardIds)
    if err != nil {
        return err
    }
    resp, err := anki.Request("getReviewsOfCards", map[string]interface{}{
        "cards": cardIds,
    })
    if err != nil {
        err = fmt.Errorf("error in sending addNote request: %s", err.Error())
        return err
    }

    err = anki.UpdateCache(cardIds)
    if err != nil {
        err = fmt.Errorf("error in VerifyAndCleanWithAnki: %s\n", err.Error())
        return err
    }

    lastReviews := make(map[int]anki.Review)

    for cardIdString, reviewsMarshalled := range resp.Result.(map[string]interface{}) {
        cardId, err := strconv.ParseInt(cardIdString, 10, 64)
        if err != nil {
            err = fmt.Errorf("error in VerifyAndCleanWithAnki: %s", err.Error())
            return err
        }
        if sort.SearchInts(cardIds, int(cardId)) == len(cardIds) {
            err = fmt.Errorf("error in VerifyAndCleanWithAnki: result of getReviewsOfCards has invalid cardId=%d\n", cardId)
            return err
        }
        var lastReview anki.Review

        for _, reviewMarshalled := range reviewsMarshalled.([]interface{}) {
			reviewMap, ok := reviewMarshalled.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected type for reviewMarshalled: %T", reviewMarshalled)
			}
			fmt.Println("Anki reviewMap=",reviewMap)
            var ankiReview anki.Review
			ankiReview.ReviewTime = int(reviewMap["id"].(float64))
			ankiReview.Usn = reviewMap["usn"]
			ankiReview.ButtonPressed = reviewMap["ease"]
			ankiReview.NewInterval = int(reviewMap["ivl"].(float64))
			ankiReview.PreviousInterval = int(reviewMap["lastIvl"].(float64))
			ankiReview.NewFactor = reviewMap["factor"]
			ankiReview.ReviewDuration = reviewMap["time"]
			ankiReview.ReviewType = reviewMap["type"]
            //if err := json.Unmarshal(reviewMarshalled.([]byte), &ankiReview); err != nil {
            //    return fmt.Errorf("error unmarshalling json getReviewsOfCards response for cardId=%d: %s\n", cardId, err.Error())
            //}
            if (ankiReview.ReviewTime > lastReview.ReviewTime) {
                lastReview = ankiReview
            }
        }
        lastReviews[int(cardId)] = lastReview
    }

    // With the last Review information
    // How do I convert ReviewTime to Time?
    var cardsToDeleteFromAnki []int
    for cardId, lastReview := range lastReviews {
        elemInfo, exists, err := anki.FindElemInfoWithCardId(cardId)
		if !exists {
			// TODO nocheckin handle this case!
			err = fmt.Errorf("error anki.FindElemInfoWithCardId(%d) does not exist!", cardId)
			return err
		}
        if err != nil {
            // TODO log this error! I don't know why these aren't being found
            // nocheckin
            resp, err2 := anki.Request("cardsInfo", map[string]interface{}{"cards": []interface{}{cardId}})
            if err2 != nil {
                return fmt.Errorf("error in sending `cardsInfo` anki.Request: %v", err2)
            }
            fmt.Println(resp.Result)

            return err
        }
        if sort.SearchStrings(uuidsEmacs, elemInfo.Uuid) == len(uuidsEmacs) {
            //TODO delete the anki card
            cardsToDeleteFromAnki = append(cardsToDeleteFromAnki, cardId)

        }
        elemInfo.TopicInfo.LastReview = lastReview.GetReviewTime()
        elemInfo.TopicInfo.Interval = float64(lastReview.NewInterval)
        dueDate, err := elemInfo.TopicInfo.DueDate()
        if err != nil {
            return err
        }
        fmt.Println("dueDate = ", dueDate)
        if err := UpdateOrInsertWithElemInfo(elemInfo, true); err != nil {
            err = fmt.Errorf("error with UpdateOrInsertWithElemInfo: %s\n", err.Error())
            return err
        }
    }

    // Delete cards not in emacs
    if err := anki.DeleteCards(cardsToDeleteFromAnki); err != nil {
        fmt.Println("Failed to delete these: ", cardsToDeleteFromAnki)
        err = fmt.Errorf("error Deleting Anki Cards: %s\n", err.Error())
        return err
    }

    return nil
}

func Init() error {
    // Now read the database
	homeDir, err := os.UserHomeDir()
	if err != nil {
        return err
	}

	ElementsDbPath = filepath.Join(homeDir, ".local", "share", "org-sm", "elements.db")
	if err := os.MkdirAll(filepath.Dir(ElementsDbPath), os.ModePerm); err != nil {
        return err
    }

    // Create and connect to the SQLite database
    db, err := sql.Open("sqlite3", ElementsDbPath)
    if err != nil {
        return err
    }

    // Check the connection
    if err := db.Ping(); err != nil {
        return err
    }

    // Create the Element table if it doesn't exist
    createTableSQL := `
    CREATE TABLE IF NOT EXISTS Element (
        uuid TEXT PRIMARY KEY,
        elementType TEXT,
        cardId INTEGER,
        lastReview DATETIME,
        interval REAL,
        status INTEGER
    );
	CREATE TABLE IF NOT EXISTS State (
		id INTEGER PRIMARY KEY,
		last_sync_date TEXT
	);`

    if _, err = db.Exec(createTableSQL); err != nil {
        return err
    }

    middlewareDb.db = db

    return nil
}

func FindUuids() (uuids []string, err error) {
	query := `
	SELECT uuid
	FROM Element
    WHERE status = 1;`

	rows, err := middlewareDb.db.Query(query)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var uuid string
		if err := rows.Scan(&uuid); err != nil {
			log.Fatal(err)
		}
        uuids = append(uuids, uuid)
	}

	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}
    return
}

func UpdateOrInsertWithElemInfo(elemInfo emacs.ElemInfo, updateTopicInfo bool) error {
    if updateTopicInfo {
        statement := `
        INSERT OR IGNORE INTO Element (uuid, cardId, elementType, status, lastReview, interval)
        VALUES (?, ?, ?, ?, ?, ?);
        UPDATE Element SET elementType=?, status=?, lastReview=?, interval=? WHERE uuid LIKE ?;`

        _, err := middlewareDb.db.Exec(
            statement,
            elemInfo.Uuid,
            elemInfo.CardId,
            elemInfo.ElementType,
            elemInfo.Status,
            elemInfo.TopicInfo.LastReview.Format("2006-01-02 15:04:05"),
            elemInfo.TopicInfo.Interval,

            elemInfo.ElementType,
            elemInfo.Status,
            elemInfo.TopicInfo.LastReview.Format("2006-01-02 15:04:05"),
            elemInfo.TopicInfo.Interval,
            elemInfo.Uuid,
        )
        if err != nil {
            return fmt.Errorf("error updating or inserting element: %v", err)
        }
    } else {
        statement := `
        INSERT OR IGNORE INTO Element (uuid, cardId, elementType, status, lastReview, interval)
        VALUES (?, ?, ?, ?, ?, ?);
        UPDATE Element SET elementType=?, status=? WHERE uuid LIKE ?;`

        _, err := middlewareDb.db.Exec(
            statement,
            elemInfo.Uuid,
            elemInfo.CardId,
            elemInfo.ElementType,
            elemInfo.Status,
            elemInfo.TopicInfo.LastReview.Format("2006-01-02 15:04:05"),
            elemInfo.TopicInfo.Interval,

            elemInfo.ElementType,
            elemInfo.Status,
            elemInfo.Uuid,
        )
        if err != nil {
            return fmt.Errorf("error updating or inserting element: %v", err)
        }
    }

	return nil
}

func UpdateLearningQueue() (err error) {
	learningQueueMutex.Lock()
	defer learningQueueMutex.Unlock()
	fmt.Println("Updating LQ")
	learningQueue, err = FindDueElements()
	return
}

func FindDueElements() (elemInfos []emacs.ElemInfo, err error) {
	// Find due items
    cardIds, err := anki.FindDueCards(middlewareDb.db)
    if err != nil {
        return
    }
    for _, cardId := range cardIds {
		ei, exists, err_ := anki.FindElemInfoWithCardId(cardId)
		if err_ != nil {
            fmt.Println("error FindElemInfoWithCardId: ", err_)
			err = err_
			return
		}
        if !exists {
            err = fmt.Errorf("error: Did not find elemInfo for cardId=%d\n", cardId)
            fmt.Println("error: ", err)
            return
        }
		if ei.IsDismissed() {
			continue
		}
		elemInfos = append(elemInfos, ei)
		fmt.Println("Found card: ", ei)
    }

	// Find due topics
    topicElemInfos, err := FindDueTopics()
    if err != nil {
        return
    }
    elemInfos = append(elemInfos, topicElemInfos...)

    // Now sort the elements based on priority and some noisiness
    sort.Slice(elemInfos, func(i, j int) bool {
        // TODO make the randomness configurable
        noiseI := float64(elemInfos[i].Priority) + (rand.Float64()*2.0 - 1.0)*12.0
        noiseJ := float64(elemInfos[j].Priority) + (rand.Float64()*2.0 - 1.0)*12.0
        return noiseI < noiseJ
    })

    return
}

func UpdateWithTopicInfo(ei *emacs.ElemInfo) (err error) {
	query := `
        SELECT lastReview, interval, elementType
        FROM Element
        WHERE uuid = ?;
    `

	var lastReview time.Time
	var interval float64
	var elementType string

	err = middlewareDb.db.QueryRow(query, ei.Uuid).Scan(&lastReview, &interval, &elementType)
	if err != nil {
		return
	}
	if elementType != emacs.ElementTypeTopic {
		err = fmt.Errorf("row found for ei is not topic: ei=%v", ei)
		return
	}

	ei.TopicInfo.LastReview = lastReview
	ei.TopicInfo.Interval = interval

	return nil
}

func FindDueTopics() (elemInfos []emacs.ElemInfo, err error) {
	query := `
	SELECT uuid, elementType, lastReview, interval
	FROM Element
	WHERE datetime(lastReview, '+' || interval || ' days') < datetime('now', 'start of day')
    AND status = 1
	AND elementType = ':topic';`

	rows, err := middlewareDb.db.Query(query)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

    var uuidsToDelete []string

	for rows.Next() {
		var uuid, elementType string
		var lastReview time.Time
		var interval float64
		if err := rows.Scan(&uuid, &elementType, &lastReview, &interval); err != nil {
			log.Fatal(err)
		}

        // TODO priority
        elemInfo, exists, err := emacs.FindElemInfo(uuid)
        if err != nil {
            fmt.Println("error: ", err)
            uuidsToDelete = append(uuidsToDelete, uuid)
            continue
        }

		if !exists {
			continue
		}

		topicInfo := emacs.TopicInfo {
			Interval: interval,
			LastReview: lastReview,
		}
		elemInfos = append(elemInfos, emacs.ElemInfo {
			Uuid: uuid,
			Priority: elemInfo.Priority,
			AFactor: elemInfo.AFactor,
			ElementType: ":topic",
			CardId: 0,
			TopicInfo: topicInfo,
			Status: emacs.StatusEnabled,
		})
		fmt.Printf("UUID: %s, ElementType: %s, LastReview: %s, Interval: %d\n", uuid, elementType, lastReview, interval)
	}

	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}

    // TODO ok now, you need to delete uuidsToDelete
    if len(uuidsToDelete) > 0 {
        if err = DeleteEntries(uuidsToDelete, DeleteDefault); err != nil {
            log.Fatal(err)
        }
    }

    return
}

func CurrentElement() (ei emacs.ElemInfo, err error) {
	ei = LearningQueue()[0]
	if ei.IsTopic() {
		return ei, nil
	} else {
		// Instead of returning the item from the learningQueue, we will return the anki
		var noDueExists bool
		ei, noDueExists, err = anki.CurrentElemInfo()
		if err != nil {
			return
		}
		if noDueExists {
			LearningQueuePopFront()
			ei, err = CurrentElement()
			return
		}
		return
	}
}

// Use `Anki.FindCardIdsInAnki()`

const (
    DeleteDefault = 1
)

func DeleteEntries(uuids []string, deleteType int) error {
    // TODO add deleted uuids to the approriate log

    if err := DeleteEntriesFromDb(uuids); err != nil {
        return err
    }
    if err := anki.DeleteEntriesWithUuids(uuids); err != nil {
        return err
    }
    return nil
}

func DeleteEntriesFromDb(uuids []string) error {
    db := middlewareDb.db

    // Begin a transaction
    tx, err := db.Begin()
    if err != nil {
        return fmt.Errorf("Error beginning transaction: %w", err)
    }

    // Build the DELETE query with the appropriate number of placeholders
    placeholders := strings.Repeat("?,", len(uuids))
    placeholders = placeholders[:len(placeholders)-1] // Remove the trailing comma
    query := fmt.Sprintf("DELETE FROM Element WHERE uuid IN (%s)", placeholders)

    // Convert uuids to a slice of interface{} for the Exec function
    args := make([]interface{}, len(uuids))
    for i, uuid := range uuids {
        args[i] = uuid
    }

    // Execute the query within the transaction
    result, err := tx.Exec(query, args...)
    if err != nil {
        tx.Rollback()
        return fmt.Errorf("Error executing query: %w", err)
    }

    // Commit the transaction
    if err := tx.Commit(); err != nil {
        return fmt.Errorf("Error committing transaction: %w", err)
    }

    // Check how many rows were affected
    rowsAffected, err := result.RowsAffected()
    if err != nil {
        return fmt.Errorf("Error getting rows affected: %w", err)
    }

    fmt.Printf("Number of rows affected: %d\n", rowsAffected)
    return nil
}

func Close() {
    db := middlewareDb.db
    defer db.Close()
}
