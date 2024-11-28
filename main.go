package main

import "middleware/anki"
import "middleware/emacs"

import (
	"io"
	"strconv"
	"errors"
	"fmt"
	"net/http"
	"time"
	"encoding/json"
	"strings"
	"os/exec"
	"os"
	"math/rand"
	"sort"
	"path/filepath"
	_ "reflect"
	"database/sql"
	"log"
    _ "github.com/mattn/go-sqlite3"
)

var DEBUG_HB bool // nocheckin

/*
https://docs.ankiweb.net/searching.html
https://foosoft.net/projects/anki-connect/
*/

const (
    emacsServerPort = 31336
    //maxRetries  = 5 // TODO nocheckin delete
    // retryDelay  = 100 * time.Millisecond // delete
    //busyTimeout = 5000 // in milliseconds
)

var learningQueue []emacs.ElemInfo
var currentWasGraded bool

type MiddlewareDb struct {
    db                  *sql.DB
}

func (m *MiddlewareDb) DidVerifyAndCleanToday() (bool, error) {
	var lastSyncDate string

	// Query to fetch last sync date
	row := m.db.QueryRow("SELECT last_sync_date FROM State WHERE id = 1")
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

type PayloadUuid struct {
    Uuid string `json:"uuid"`
}
type PayloadSetGrade struct {
    Grade int `json:"grade"`
}

//var middlewareDb MiddlewareDb // nocheckin
//var currentElementUuid string // nocheckin

var ElementsDbPath string

// Now I need to cache the results from emacs and have an emacs server.
// I need to get the Priority from emacs. I'll have a default A-factor of 1.2.
// I need a database for topic reviews and stuff.... This all should be in its own server perhaps, then.

func main() {
    var middlewareDb MiddlewareDb

    rand.Seed(time.Now().UnixNano())
    anki.CardIdCache = make(map[string]int)
    anki.UuidCache   = make(map[int]string)
    emacs.ElemInfoCache = make(map[string]emacs.ElemInfo)

    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
    done := make(chan bool)

    // Run the loop in a separate goroutine
    go func() {
        for {
            select {
            case <-ticker.C:
                // Replace the above line with the actual action you want to perform
				fmt.Print(".")
				if err := middlewareDb.HeartBeat(); err != nil {
					fmt.Print("\n")
					err = fmt.Errorf("Error in HeartBeat: %v", err)
					log.Fatal(err)
				}

			case <-done:
                // Exit the loop when done
				fmt.Print(" Done\n")
                return
            }
        }
    }()

    // Start the http server for listening to requests from emacs.
    go func() {
        mux := http.NewServeMux()
        handlers := make(map[string]http.HandlerFunc)

        // Helper to register handlers and store their routes
        register := func(pattern string, handler http.HandlerFunc) {
            mux.HandleFunc(pattern, handler)
            handlers[pattern] = handler
        }
        register("/dbg-currenteleminfo", func(w http.ResponseWriter, r *http.Request) {
			ei, err := anki.CurrentElemInfo()
			fmt.Println(ei, ", ", err)
            w.Write([]byte("{\"result\":\"true\"}"))
		})
        register("/dbg-initialize-for-testing", func(w http.ResponseWriter, r *http.Request) {
			_ = anki.VerifyConnectionAndDb()
			res := anki.DebugUuidCacheDb()
			fmt.Println("res === ", res)
			TEST2(&middlewareDb)

            w.Write([]byte("{\"result\":\"true\"}"))
		})
        register("/dbg-showpq", func(w http.ResponseWriter, r *http.Request) {
			/*
			   What this will do:
			   It will show the priority queue.

			   The priority queue will consist of topics and items:
				items are grabbed from live anki
                topics are grabbed from emacs DB.
			 */
			//fmt.Println(ei, ", ", err)
			TEST2(&middlewareDb)
            w.Write([]byte("{\"result\":\"true\"}"))
		})
		DEBUG_HB = true//nocheckin remove this stuff
        register("/dbg-fluff", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/dbg-fluff -- Enabling HeartBeat")
			DEBUG_HB = true
            _, _, err := anki.FindCard("0bc05bcf-cf9f-4e11-8d75-0adc4e1ec911")
            if err != nil {
				err = fmt.Errorf("Error from anki.FindCard: %v", err)
				fmt.Println("nocheckin: ", err)
            }
			// TODO nocheckin delete
			//ei, err := anki.CurrentElemInfo()
			//fmt.Println(ei, ", ", err)
            w.Write([]byte("{\"result\":\"true\"}"))
		})
        register("/extract", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/extract")
            // TODO nocheckin : change /extract so that it takes the original uuid of the card you're extracting
            // TODO nocheckin : make /extract create new element in database and possibly anki? (no, because result always is topic?)
            body, err := io.ReadAll(r.Body)
            if err != nil {
                http.Error(w, "Unable to read body", http.StatusBadRequest)
                return // TODO why do i stop the server here? not robust
            }
            defer r.Body.Close()
            var payload PayloadUuid
            if err := json.Unmarshal(body, &payload); err != nil {
                http.Error(w, "Unable to parse JSON", http.StatusBadRequest)
                return
            }
			fmt.Println("nocheckin", payload.Uuid)
			// TODO now we need to do the rest of this
            w.Write([]byte("{\"result\":\"true\"}"))
        })
        register("/set-grade", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/set-grade")
            body, err := io.ReadAll(r.Body)
            if err != nil {
                http.Error(w, "Unable to read body", http.StatusBadRequest)
                return // TODO why do i stop the server here? not robust
            }
            defer r.Body.Close()
            var payload PayloadSetGrade
            if err := json.Unmarshal(body, &payload); err != nil {
                http.Error(w, "Unable to parse JSON", http.StatusBadRequest)
                return
            }
			currentWasGraded = true
			if err := anki.SetGrade(payload.Grade + 1); err != nil {
                http.Error(w, "Error with answering grade", http.StatusBadRequest)
				fmt.Println("error in SetGrade(): ", err)
				return
			}
            w.Write([]byte("{\"result\":\"true\"}"))
        })
        register("/element-info", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/element-info")
			ei, err := CurrentElement()
			if err != nil {
				log.Fatal("Error in /element-info: ", err)
			}
			elementTypeS := "Topic"
			if ei.IsItem() {
				elementTypeS = "Item"
			}
			elementStatusS := "Memorized"
			if ei.IsDismissed() {
				elementStatusS = "Dismissed"
			}
			priorityS := strconv.Itoa(ei.Priority)
			afactorS := strconv.FormatFloat(ei.AFactor, 'f', -1, 64)
            w.Write([]byte(
				//TODO make sure to sanitize/escape these values - nocheckin
				"{\"Content\":\"" + "TODO" + "\"," +
				"\"ElementType\":\"" + elementTypeS + "\"," +
				"\"ElementStatus\":\"" + elementStatusS + "\"," +
				"\"Priority\":\"" + priorityS + "\"," +
				"\"AFactor\":\"" + afactorS + "\"," +
				"\"Title\":\"" + "TODO" + "\"," +
				"\"Answer\":\"" + "TODO" + "\"" +
				"}"))
        })
        register("/learning-mode", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/learning-mode")
			if anki.InReview() && len(learningQueue) > 0 {
				w.Write([]byte("{\"result\":\"standard\"}"))
			} else {
				w.Write([]byte("{\"result\":\"none\"}"))
			}
        })
        register("/next-repetition", func(w http.ResponseWriter, r *http.Request) {
            // TODO nocheckin : gets next element in the queue
			fmt.Println("\n/next-repetition TODO !!!!!!")
			currentWasGraded = false
			ei, err := CurrentElement()
			if err != nil {
				log.Fatal("Error in /next-repetition: ", err)
			}
			if ei.IsItem() {
				log.Fatal("next-repetition for Items not implemented yet")
			} else {
				ei.UpdateTopicInfo()
				fmt.Println("Updated ei: ", ei)
				middlewareDb.UpdateOrInsertWithElemInfo(ei, true)
			}
			learningQueue = learningQueue[1:]
			/*
			   TODO nocheckin
			   What all needs to be done here?

			   If we're a topic, we need to update the AFactor for the topic and stuff and we need to pop the learning queue and move on.

			   If we're an item, we need to add the grading and pop the learning queue and move on

			   lastReview

			*/
			//
            w.Write([]byte("{\"result\":\"true\"}"))
        })
        register("/next-element", func(w http.ResponseWriter, r *http.Request) {
			ei, err := CurrentElement()
			if err != nil {
				log.Fatal("Error in /next-element: ", err)
			}
			if ei.IsItem() {
				fmt.Println("next-element has found an Item.")
				//log.Fatal("next-element for Items not implemented yet")
				// I don't need to do anythign here because CurrentElement will call anki's currentCard which handles the code for getting ready for grading
			}
			fmt.Println("\n/next-element")
			// Does this even need to do anything?
            w.Write([]byte("{\"result\":\"true\"}"))
        })
        register("/append-comment", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/append-comment TODO")
            // TODO nocheckin : sets uuid of current item i guess
            w.Write([]byte("{\"result\":\"true\"}"))
        })
        register("/dismiss", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/dismiss")
			// TODO
			ei, err := CurrentElement()
			ei.Status = emacs.StatusDisabled
			if err != nil {
				log.Fatal("Error in /dismiss (CurrentElement): ", err)
			}
			middlewareDb.UpdateOrInsertWithElemInfo(ei, false)
			if ei.IsItem() {
				if err := anki.DeleteCard(ei.CardId); err != nil {
					log.Fatal("Fatal: Failed to delete anki card, with cardId=", ei.CardId)
				}
			}
			learningQueue = learningQueue[1:]
            w.Write([]byte("{\"result\":\"true\"}"))
        })
        register("/comment", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/comment")
			ei, err := CurrentElement()
			if err != nil {
				log.Fatal("Error in /comment (CurrentElement): ", err)
			}
			uuid := ei.Uuid
            w.Write([]byte("{\"result\":\"org-sm-registered<" + uuid + ">\"}"))
        })
        register("/begin-learning", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/begin-learning")
			if err := anki.StartReview(); err != nil {
				err = fmt.Errorf("error from StartReview: %s", err.Error())
				log.Fatal(err)
			}
			var err error
			learningQueue, err = middlewareDb.FindDueElements()
			if err != nil {
				err = fmt.Errorf("error from FindDueElements: %s", err.Error())
				log.Fatal(err)
			}
            w.Write([]byte("{\"result\":\"true\"}"))
        })
        register("/was-graded", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/was-graded")
            // TODO nocheckin
			if currentWasGraded {
				w.Write([]byte("{\"result\":\"true\"}"))
			} else {
				w.Write([]byte("{\"result\":\"false\"}"))
			}
        })
        register("/ready-to-grade", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/ready-to-grade")
            // TODO nocheckin: maybe I need to make this confirm anki is connected?
            w.Write([]byte("{\"result\":\"true\"}"))
        })
        register("/new-topic", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/new-topic TODO !!!!!!")
            // TODO nocheckin
            w.Write([]byte("{\"result\":\"true\"}"))
        })
        register("/new-item", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/new-item TODO !!!!!!")
            // TODO nocheckin
            w.Write([]byte("{\"result\":\"true\"}"))
        })
        register("/goto-first-element-with-comment", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/goto-first-element-with-comment TODO !!!!!!")
            // TODO nocheckin : this switches the anki GUI to the corresponding UUID
            w.Write([]byte("{\"result\":\"true\"}"))
        })

		// Root handler to list all registered handlers
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			html := "<html><body><h1>Registered Handlers:</h1>"
			for pattern := range handlers {
				html += fmt.Sprintf(`<a href="%s">%s</a><br>`, pattern, pattern)
			}
			html += "</body></html>"
			fmt.Fprint(w, html)
		})

        fmt.Printf("Listening on port %d. http://localhost:31336/\n", emacsServerPort)
        server := http.Server {
            Addr:    fmt.Sprintf(":%d", emacsServerPort),
                Handler: mux,
            }
        if err := server.ListenAndServe(); err != nil {
            if !errors.Is(err, http.ErrServerClosed) {
                fmt.Printf("error running http server: %s\n", err)
            }
        }
    }()

    time.Sleep(100 * time.Millisecond) // I need to loop over and over and then check for if new VerifyAndClean is required.

    if err := middlewareDb.Init(); err != nil {
        log.Fatal(err)
    }
    db := middlewareDb.db
    defer db.Close()

	// I should check if the program has ever started running before.
	// NO I'll just use the database.
	if err := middlewareDb.HeartBeat(); err != nil {
		err = fmt.Errorf("Error in HeartBeat: %v", err)
		log.Fatal(err)
	}

    //nocheckin
    //// Now we want to write our test.
    //if err := middlewareDb.UpdateAndSyncIfRequired(); err != nil {
    //    log.Fatal(err)
    //}

    select {}
    done <- true
}

func (middlewareDb *MiddlewareDb) HeartBeat() error {
	if !DEBUG_HB {
		return nil // nocheckin
	}
	// Confirm the database exists for this software.
	homeDir, err := os.UserHomeDir()
	if err != nil {
        return err
	}
	ElementsDbPath = filepath.Join(homeDir, ".local", "share", "org-sm", "elements.db")
	if _, err := os.Stat(ElementsDbPath); os.IsNotExist(err) {
		if err := middlewareDb.Init(); err != nil {
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

	//
	if err := middlewareDb.UpdateAndSyncIfRequired(); err != nil {
		err = fmt.Errorf("error in UpdateAndSyncIfRequired: %v", err)
		return err
	}

	return nil
}

func TEST2(middlewareDb *MiddlewareDb) {
    if err := anki.StartReview(); err != nil {
        err = fmt.Errorf("error from StartReview: %s", err.Error())
        log.Fatal(err)
    }

	var err error
    learningQueue, err = middlewareDb.FindDueElements()
    if err != nil {
        err = fmt.Errorf("error from FindDueElements: %s", err.Error())
        log.Fatal(err)
    }
    fmt.Println(CurrentElement())

    //var front ElemInfo
    //while len(queue) > 0 {
    //    if queue[0].IsTopic() {
    //    }
    //    front, queue = queue[0], queue[1:]
    //    //
    //}

}

func (m *MiddlewareDb) UpdateAndSyncIfRequired() error {
	didClean, err := m.DidVerifyAndCleanToday()
	if err != nil {
		err = fmt.Errorf("error from DidVerifyAndCleanToday: %v", err)
		return err
	}
	if didClean {
		// Skip since we already did it today
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

    if err := m.VerifyAndCleanWithEmacs(uuidsEmacs); err != nil {
		err = fmt.Errorf("error from VerifyAndCleanWithEmacs: %v", err)
        return err
    }
    if err := m.VerifyAndCleanWithAnki(uuidsEmacs); err != nil {
		err = fmt.Errorf("error from VerifyAndCleanWithAnki: %v", err)
        return err
    }

	// Update the learning queue
	learningQueue, err = m.FindDueElements()
	if err != nil {
        err = fmt.Errorf("error from FindDueElements: %s", err.Error())
        log.Fatal(err)
    }

	if err := m.UpdateLastVerifyAndCleanDate(); err != nil {
		err = fmt.Errorf("error from UpdateLastVerifyAndCleanDate: %v", err)
		return err
	}

	// Next update the queue

    return nil
}

func (m *MiddlewareDb) UpdateLastVerifyAndCleanDate() error {
	syncDate := time.Now().Format("2006-01-02")

	// Upsert the sync date
	_, err := m.db.Exec(`INSERT INTO State (id, last_sync_date) VALUES (1, ?)
					  ON CONFLICT(id) DO UPDATE SET last_sync_date = excluded.last_sync_date`, syncDate)
	if err != nil {
		return err
	}
	return nil
}

// TODO rename this. Because it also does stuff for anki. I can think of better names for these helper functions.
// TODO add DOC COMMENT that explains that the uuidsEmacs argument must be pre-sorted before being passed in
func (m *MiddlewareDb) VerifyAndCleanWithEmacs(uuidsEmacs []string) error {
    // TODO nocheckin : I think here I should handle Anki. Add "org-sm-skip" and "org-sm-flag-corrupt" tags to anki
    // Remove entries from Middlware DB which are not present in any emacs elements.
    fmt.Println("Finding Uuids in MWdb")
    uuidsMwdb, err := m.FindUuids()
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
        if err = m.DeleteEntries(uuidsToDelete, DeleteDefault); err != nil {
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
                if err = anki.CreateCard(elemInfo.Uuid, ""/*TODO nocheckin  , elemInfo.content*/); err != nil {
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
				fmt.Println("nocheckin: ", err)
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

        if err := m.UpdateOrInsertWithElemInfo(elemInfo, false); err != nil {
			err = fmt.Errorf("error from UpdateOrInsertWithElemInfo: %v", err)
			return err
        }
    }

    return nil
}

func (m *MiddlewareDb) VerifyAndCleanWithAnki(uuidsEmacs []string) error {
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
		//fmt.Printf("prior: unmarshaling review: %s\n", reviewMarshalled) // TODO nocheckin [1021]

        for _, reviewMarshalled := range reviewsMarshalled.([]interface{}) {
			reviewMap, ok := reviewMarshalled.(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected type for reviewMarshalled: %T", reviewMarshalled)
			}
			fmt.Println("reviewMap=",reviewMap)
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
			// TODO nocheckin handle htis case!
			err = fmt.Errorf("error anki.FindElemInfoWithCardId(%d) does not exist!", cardId)
			return err
		}
        if err != nil {
            // TODO log this error! I don't know why these aren't being found
            // nocheckin
            resp, err := anki.Request("cardsInfo", map[string]interface{}{"cards": []interface{}{cardId}})
            if err != nil {
                err = fmt.Errorf("error in sending `cardsInfo` anki.Request: %v", err)
                return err
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
        if err := m.UpdateOrInsertWithElemInfo(elemInfo, true); err != nil {
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

func (m *MiddlewareDb) Init() error {
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

    m.db = db

    return nil
}

func (m *MiddlewareDb) FindUuids() (uuids []string, err error) {
	query := `
	SELECT uuid
	FROM Element
    WHERE status = 1;`

	rows, err := m.db.Query(query)
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

func (m *MiddlewareDb) UpdateOrInsertWithElemInfo(elemInfo emacs.ElemInfo, updateLastReviewInfo bool) error {
    if updateLastReviewInfo {
        statement := `
        INSERT OR IGNORE INTO Element (uuid, cardId, elementType, status, lastReview, interval)
        VALUES (?, ?, ?, ?, ?, ?);
        UPDATE Element SET elementType=?, status=?, lastReview=?, interval=? WHERE uuid LIKE ?;`

        _, err := m.db.Exec(
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

        _, err := m.db.Exec(
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

func (m *MiddlewareDb) UpdateOrInsertElementDbEntry(uuid string, elementType string, lastReview time.Time, interval float64, status int) error {
	log.Fatal("This function is currently not being used anywhere, right?")//nocheckin
	// Prepare the insert or replace statement
	insertStatement := `
	INSERT OR REPLACE INTO Element (uuid, elementType, lastReview, interval, status)
	VALUES (?, ?, ?, ?, ?);`

	// Execute the insert or replace statement
	_, err := m.db.Exec(insertStatement, uuid, elementType, lastReview.Format("2006-01-02 15:04:05"), interval, status)
	if err != nil {
		return fmt.Errorf("error updating or inserting element: %v", err)
	}

	return nil
}

func (m *MiddlewareDb) FindDueElements() (elemInfos []emacs.ElemInfo, err error) {
	// Find due items
    cardIds, err := anki.FindDueCards(m.db)
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

		elemInfos = append(elemInfos, ei)
		fmt.Println("Found card: ", ei)
    }

	// Find due topics
    topicElemInfos, err := m.FindDueTopics()
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

func CurrentElement() (ei emacs.ElemInfo, err error) {
	if learningQueue[0].IsTopic() {
		return learningQueue[0], nil
	} else {
		// Instead of returning the item from the learningQueue, we will return the anki
		ei, err = anki.CurrentElemInfo()
		if err != nil {
			return
		}
		return
	}
}

func (m *MiddlewareDb) FindDueTopics() (elemInfos []emacs.ElemInfo, err error) {
	query := `
	SELECT uuid, elementType, lastReview, interval
	FROM Element
	WHERE datetime(lastReview, '+' || interval || ' days') < datetime('now', 'start of day')
    AND status = 1
	AND elementType = ':topic';`

	rows, err := m.db.Query(query)
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
        if err = m.DeleteEntries(uuidsToDelete, DeleteDefault); err != nil {
            log.Fatal(err)
        }
    }

    return
}

// Use `Anki.FindCardIdsInAnki()`

const (
    DeleteDefault = 1
)

func (m *MiddlewareDb) DeleteEntries(uuids []string, deleteType int) error {
    // TODO add deleted uuids to the approriate log

    if err := m.DeleteEntriesFromDb(uuids); err != nil {
        return err
    }
    if err := anki.DeleteEntriesWithUuids(uuids); err != nil {
        return err
    }
    return nil
}

func (m *MiddlewareDb) DeleteEntriesFromDb(uuids []string) error {
    db := m.db

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
