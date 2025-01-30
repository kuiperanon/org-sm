package middlewareDb

// nocheckin :: Rename this file to learningQueue ? Or something else because it also has Persist(ei) -- maybe that can go somewhere else -- Maybe there should be a "HeartBeat" or "Clean" or "Sanitizer" (ask chatgpt for good name) section and a learningQueue section

import "middleware/emacs"
import "middleware/anki"
import "middleware/elemInfo"

import (
	"os"
	"fmt"
	"sync"
	"time"
	"io/ioutil"
	"log"
	"sort"
	"math/rand"
	"path/filepath"
    _ "github.com/mattn/go-sqlite3"
)

var learningQueue []elemInfo.ElemInfo
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

func LearningQueue() []elemInfo.ElemInfo {
	learningQueueMutex.Lock()
	defer learningQueueMutex.Unlock()
	return learningQueue
}

func DidVerifyAndCleanToday() (bool, error) {
	// Confirm the database exists for this software.
	homeDir, err := os.UserHomeDir()
	if err != nil {
        return false, err
	}

	// Create path if not existing.
	filePath := filepath.Join(homeDir, ".local", "share", "org-sm", "date.txt")
	if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
        return false, err
    }

	// If file doesn't exist
	_, err = os.Stat(filePath)
	if os.IsNotExist(err) {
		return false, nil
	}

	// Read the file contents
	lastSyncDateString, err := ioutil.ReadFile(filePath)
	if err != nil {
        return false, err
	}

	lastSyncDate := string(lastSyncDateString)

	date := time.Now().Format("2006-01-02")
	didToday := date == lastSyncDate

	return didToday, nil
}

var HeartBeatRunning bool

func HeartBeat() error {
    if HeartBeatRunning {
        return nil
    }
    HeartBeatRunning = true

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
		// TODO nocheckin :: This will continually call this over and over. Don't let this happen
		// Process the org files and (re)generate/update the ElemInfoCache
		err := emacs.PullDataFromOrgFiles()
		if err != nil {
			return err
		}
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
	didClean, err := DidVerifyAndCleanToday()
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

	// TODO nocheckin :: Make it so this happens when you start up the program.
	if err := anki.UpdateCaches(); err != nil {
		err = fmt.Errorf("error from UpdateCaches: %v", err)
		return err
	}

    uuidsEmacs := emacs.FindUuidsSorted()

	// Update Anki
    if err := DeleteAnkiCardsNotInEmacs(uuidsEmacs); err != nil {
		err = fmt.Errorf("error from DeleteAnkiCardsNotInEmacs: %v", err)
        return err
    }
    if err := ExportItemsToAnki(uuidsEmacs); err != nil {
		err = fmt.Errorf("error from ExportItemsToAnki: %v", err)
        return err
    }
	// TODO nocheckin :: implement this: "Removing entries from anki.db UuidCache which are missing from Anki..."


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
	fmt.Println("Finished with Update and Sync!")

    return nil
}

	// nocheckin :: confirm this works
func UpdateLastVerifyAndCleanDate() error {
	syncDate := time.Now().Format("2006-01-02")

    // Now read the database
	homeDir, err := os.UserHomeDir()
	if err != nil {
        return err
	}

	// Open or create the file
	file, err := os.Create(filepath.Join(homeDir, ".local", "share", "org-sm", "date.txt"))
	if err != nil {
		fmt.Println("Error creating file:", err)
		return err
	}
	defer file.Close()

	// Write the date to the file without a newline
	_, err = file.WriteString(syncDate)
	if err != nil {
		fmt.Println("Error writing to file:", err)
		return err
	}

	return nil
}

func ExportItemsToAnki(uuidsEmacs []string) error {
    fmt.Println("Finding and exporting items missing from Anki...")
	defer fmt.Println("Finished with finding and exporting items missing from Anki...")

    // Update the Middleware DB with Emacs data
    for _, uuid := range uuidsEmacs {
        elemInfo, elemExists, err := emacs.FindElemInfo(uuid)
		if err != nil {
			err = fmt.Errorf("error from emacs.FindElemInfo: %v", err)
			return err
		}
		if !elemExists {
			// TODO nocheckin :: This is weird, I should log an error here. Dunno why I would just continue...
			continue
		}

        // Create Card in Anki if it doesn't exist
        if elemInfo.IsItem() {
            _, cardExists, err := anki.FindCard(uuid)
            if err != nil {
				err = fmt.Errorf("Error from anki.FindCard: %v", err)
				fmt.Println(err)
                return err
            }
            if !cardExists {
				fmt.Println("Found item missing from Anki: Creating card ", elemInfo.Uuid)
                if err = anki.CreateCard(elemInfo.Uuid, ""); err != nil {
                    return err
                }
            }
        }
    }

    return nil
}

func DeleteAnkiCardsNotInEmacs(uuidsEmacs []string) error {
	// Go through all cardId and confirm that they exist in emacs, otherwise delete them.

	// TODO nocheckin :: Also go through and delete any cards that are Dismissed!!

    fmt.Println("Finding and deleting Anki cards which are missing from Emacs or dismissed..")
	defer fmt.Println("Finished with finding and deleting Anki cards which are missing from Emacs or dismissed...")

    cardIds, err := anki.FindCards()
    sort.Ints(cardIds)
    if err != nil {
        return err
    }

    var cardsToDeleteFromAnki []int
    for _, cardId := range cardIds {
        elemInfo, exists, err := anki.FindElemInfoWithCardId(cardId)
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
		if !exists {
            cardsToDeleteFromAnki = append(cardsToDeleteFromAnki, cardId)
		} else if sort.SearchStrings(uuidsEmacs, elemInfo.Uuid) == len(uuidsEmacs) {
			// Anki card's uuid is missing from emacs--So delete it
            cardsToDeleteFromAnki = append(cardsToDeleteFromAnki, cardId)
        }
    }

    // Delete cards with uuids not in emacs
    if err := anki.DeleteCards(cardsToDeleteFromAnki); err != nil {
        fmt.Println("Failed to delete these: ", cardsToDeleteFromAnki)
        err = fmt.Errorf("error Deleting Anki Cards: %s\n", err.Error())
        return err
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

func FindDueElements() (elemInfos []elemInfo.ElemInfo, err error) {
	// Find due items
    cardIds, err := anki.FindDueCards()
    if err != nil {
        return
    }
	fmt.Println("Due cards: ", cardIds)
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
		fmt.Println("Found card: ", ei.Uuid)
    }

	// Find due topics
    topicElemInfos := FindDueTopics()
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

func FindDueTopics() (elemInfos []elemInfo.ElemInfo) {
	return emacs.FindDueTopics()
}

func CurrentElement() (ei elemInfo.ElemInfo, err error) {
	// TODO nocheckin Handle zero length queue
	ei = LearningQueue()[0]
	if ei.IsTopic() {
		return ei, nil
	} else {
		// Instead of returning the item from the learningQueue, we will return the anki
		var noDueExists bool
		ei, noDueExists, err = anki.CurrentElemInfo()
		if err != nil {
			return ei, err
		}
		if noDueExists {
			//fmt.Println("Popping front of learning Queue")
			LearningQueuePopFront()
			ei, err = CurrentElement()
			return ei, err
		}
		return ei, nil
	}
}

func Persist(ei elemInfo.ElemInfo) {
	// This shall persist the Emacs and Anki cache--and it shall make the Anki database save
	//if ei.IsItem() {
	//	// Persist in anki
	//	if err := anki.Persist(ei); err != nil {
	//		log.Fatal("Failed to Persist item in anki.db UuidCache. Err = ", err)
	//	}
	//}
	//
	emacs.Persist(ei)
}
