package main

import "middleware/anki"
import "middleware/emacs"
import "middleware/middlewareDb"

import (
	"io"
	"strconv"
	"errors"
	"fmt"
	"net/http"
	"time"
	"encoding/json"
	"math/rand"
	_ "reflect"
	"log"
    _ "github.com/mattn/go-sqlite3"
)

/*
https://docs.ankiweb.net/searching.html
https://foosoft.net/projects/anki-connect/
*/

const (
    emacsServerPort = 31336
)

var currentWasGraded bool

type PayloadSetGrade struct {
    Grade int `json:"grade"`
}

type PayloadSetPriority struct {
	Uuid     string  `json:"uuid"`
    Priority float64 `json:"priority"`
}

type PayloadNewItem struct {
    Uuid string `json:"uuid"`
    Priority float64 `json:"priority"`
}
type PayloadNewTopic struct {
    Uuid string `json:"uuid"`
    Priority float64 `json:"priority"`
}

// nocheckin - notes:
// Now I need to cache the results from emacs and have an emacs server.
// I need to get the Priority from emacs. I'll have a default A-factor of 1.2.
// I need a database for topic reviews and stuff.... This all should be in its own server perhaps, then.

func main() {

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
        register("/dbg-duedate/{id}", func(w http.ResponseWriter, r *http.Request) {
			elemInfo, elemExists, err := emacs.FindElemInfo(r.PathValue("id"))
			fmt.Println("duedate ei: ", elemInfo)
			if err != nil {
				log.Fatal("dbg-duedate, err=", err)
			}
			err = middlewareDb.UpdateWithTopicInfo(&elemInfo)
			if err != nil {
				log.Fatal("dbg-duedate, err=", err)
			}
			if !elemExists {
				w.Write([]byte("{\"result\":\"noexist\"}"))
			} else {
				dueDate, err := elemInfo.DueDate()
				if err != nil {
					log.Fatal(err)
				}
				dateS := dueDate.Format("2006-01-02")
				w.Write([]byte("{\"result\":\"" + dateS + "\"}"))
			}
		})
        register("/dbg-currenteleminfo", func(w http.ResponseWriter, r *http.Request) {
			ei, _, err := anki.CurrentElemInfo()
			fmt.Println(ei, ", ", err)
            w.Write([]byte("{\"result\":\"true\"}"))
		})
        register("/dbg-initialize-for-testing", func(w http.ResponseWriter, r *http.Request) {
			_ = anki.VerifyConnectionAndDb()
			res := anki.DebugUuidCacheDb()
			fmt.Println("res === ", res)

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
			fmt.Println("LQ: ", middlewareDb.LearningQueue())
            w.Write([]byte("{\"result\":\"true\"}"))
		})
        register("/dbg-fluff", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/dbg-fluff -- Enabling HeartBeat")
            _, _, err := anki.FindCard("0bc05bcf-cf9f-4e11-8d75-0adc4e1ec911")
            if err != nil {
				err = fmt.Errorf("Error from anki.FindCard: %v", err)
				fmt.Println("nocheckin: ", err)
            }
			// TODO nocheckin delete
			//ei, _, err := anki.CurrentElemInfo()
			//fmt.Println(ei, ", ", err)
            w.Write([]byte("{\"result\":\"true\"}"))
		})
        register("/set-priority", func(w http.ResponseWriter, r *http.Request) {
			/*
			   TODO nocheckin:

			   WAIT A MINUTE, this only matters for the next day.
			   when the learningQueue is built.
			   It doesn't even get saved in the middlewareDb SQL database.

			   All I need to do is update the elemInfoCache here. No need to update the database

			   So you can delete this
			   */
			fmt.Println("\n/set-priority TODO !!!")
            body, err := io.ReadAll(r.Body)
            if err != nil {
                http.Error(w, "Unable to read body", http.StatusBadRequest)
                return // TODO why do i stop the server here? not robust
            }
            defer r.Body.Close()
            var payload PayloadSetPriority
            if err := json.Unmarshal(body, &payload); err != nil {
                http.Error(w, "Unable to parse JSON", http.StatusBadRequest)
                return
            }
			// TODO nocheckin : Make this handle items properly!!!  This will take some investigation of how to do that with anki
			elemInfo, elemExists, err := emacs.FindElemInfo(payload.Uuid)
			if err != nil {
				err = fmt.Errorf("error from emacs.FindElemInfo: %v", err)
				log.Fatal(err)
			}
			if !elemExists {
				log.Fatal("Called set-priority for uuid that doesn't exist in MIDDLEWAREDB. uid=", payload)
			}
			elemInfo.Priority = int(payload.Priority) // TODO nocheckin: confirm this is within a valid range
			fmt.Println("Setting priority: new ei= ", elemInfo)
			if err := middlewareDb.UpdateOrInsertWithElemInfo(elemInfo, false); err != nil {
				err = fmt.Errorf("error from UpdateOrInsertWithElemInfo: %v", err)
				log.Fatal(err)
			}

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
			ok, err := anki.SetGrade(payload.Grade + 1)
			if err != nil {
                http.Error(w, "Error with answering grade", http.StatusBadRequest)
				log.Fatal("error in SetGrade(): ", err)
			}
			currentWasGraded = true //nocheckin: Ignore the ok?
			if ok {
				// TODO nocheckin : Investigate how this behavior works. It seems to work even if ok is false
				// For some reason, this SEEMS to be working.
				currentWasGraded = true
			} else {
				//fmt.Println("Grading failed?")
			}
            w.Write([]byte("{\"result\":\"true\"}"))
        })
        register("/element-info", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/element-info")
			ei, err := middlewareDb.CurrentElement()
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
				// Remember to ensure these values are sanitized/escaped!
				"{\"Content\":\"" + "TODO" + "\"," +
				"\"ElementType\":\"" + elementTypeS + "\"," +
				"\"ElementStatus\":\"" + elementStatusS + "\"," +
				"\"Priority\":\"" + priorityS + "\"," +
				"\"AFactor\":\"" + afactorS + "\"," +
				"\"Title\":\"" + "TODO" + "\"," +
				"\"Answer\":\"" + "TODO" + "\"" +
				"}"))
        })
        register("/next-repetition", func(w http.ResponseWriter, r *http.Request) {
            // Gets next element in the queue
			fmt.Println("\n/next-repetition")
			currentWasGraded = false
			ei, err := middlewareDb.CurrentElement()
			if err != nil {
				log.Fatal("Error in /next-repetition: ", err)
			}
			middlewareDb.LearningQueuePopFront()
			if ei.IsItem() {
				// If it's an item, we need to reupdate the learningQueue again, to pull in the updated items.
				err = middlewareDb.UpdateLearningQueue()
				if err != nil {
					err = fmt.Errorf("error from UpdateLearningQueue: %s", err.Error())
					log.Fatal(err)
				}
			} else {
				// Update the TopicInfo with new lastReview and interval information.
				// We don't need to update this for items, because anki handles intervals.
				ei.UpdateTopicInfo()
				fmt.Println("Updated ei: ", ei)
				middlewareDb.UpdateOrInsertWithElemInfo(ei, true)
				ei.PersistInCache()
			}
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
			ei, err := middlewareDb.CurrentElement()
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
        register("/dismiss", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/dismiss")
			ei, err := middlewareDb.CurrentElement()
			if err != nil {
				log.Fatal("Error in /dismiss (CurrentElement): ", err)
			}
			fmt.Println("Dismissing element: ", ei)
			ei.Status = emacs.StatusDisabled
			middlewareDb.UpdateOrInsertWithElemInfo(ei, false)
			ei.PersistInCache()
			if ei.IsItem() {
				if err := anki.DeleteCard(ei.CardId); err != nil {
					log.Fatal("Fatal: Failed to delete anki card, with cardId=", ei.CardId)
				}
			}
			middlewareDb.LearningQueuePopFront()
            w.Write([]byte("{\"result\":\"true\"}"))
        })
        register("/current-element-id", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/current-element-id")
			ei, err := middlewareDb.CurrentElement()
			if err != nil {
				log.Fatal("Error in /current-element-id (CurrentElement): ", err)
			}
			uuid := ei.Uuid
            w.Write([]byte("{\"result\":\"org-sm-registered<" + uuid + ">\"}"))
        })
        register("/ping", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/ping")
			w.Write([]byte("{\"result\":\"true\"}"))
        })
        register("/was-graded", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/was-graded ", currentWasGraded)
			if currentWasGraded {
				w.Write([]byte("{\"result\":\"true\"}"))
			} else {
				w.Write([]byte("{\"result\":\"false\"}"))
			}
        })
        register("/new-topic", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/new-topic TODO !!!!!!")
            body, err := io.ReadAll(r.Body)
            if err != nil {
                http.Error(w, "Unable to read body", http.StatusBadRequest)
                return // TODO why do i stop the server here? not robust
            }
            defer r.Body.Close()
            var payload PayloadNewTopic
            if err := json.Unmarshal(body, &payload); err != nil {
                http.Error(w, "Unable to parse JSON", http.StatusBadRequest)
                return
            }
			log.Fatal("unimplemented") // TODO nocheckin
            w.Write([]byte("{\"result\":\"true\"}"))
        })
        register("/new-item", func(w http.ResponseWriter, r *http.Request) {
			fmt.Println("\n/newitem TODO TEST THIS")
            body, err := io.ReadAll(r.Body)
            if err != nil {
                http.Error(w, "Unable to read body", http.StatusBadRequest)
                return // TODO why do i stop the server here? not robust
            }
            defer r.Body.Close()
            var payload PayloadNewItem
            if err := json.Unmarshal(body, &payload); err != nil {
                http.Error(w, "Unable to parse JSON", http.StatusBadRequest)
                return
            }

			fmt.Println("Creating Card with payload/uuid: ", payload)
			if err = anki.CreateCard(payload.Uuid, ""); err != nil {
				log.Fatal("Fatal creating card: err=", err)
			}

			var elemInfo emacs.ElemInfo
			elemInfo.Uuid = payload.Uuid
			elemInfo.Priority = int(payload.Priority)
			elemInfo.ElementType = ":item"
			elemInfo.Status = emacs.StatusEnabled

			// Confirm we can find the card from Anki. TODO nocheckin :: Confirm I think this is also to update the cache?
			cardId, cardExists, err := anki.FindCard(payload.Uuid)
			elemInfo.CardId = cardId
			if err != nil {
				log.Fatal("Fatal err=", err)
			}
			if !cardExists {
				log.Fatal(fmt.Errorf("error: Could not find Anki card for uuid = %s\n", elemInfo.Uuid))
			}
			if err := middlewareDb.UpdateOrInsertWithElemInfo(elemInfo, false); err != nil {
				err = fmt.Errorf("error from UpdateOrInsertWithElemInfo: %v", err)
				log.Fatal(err)
			}
			elemInfo.PersistInCache()
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

    defer middlewareDb.Close()

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
