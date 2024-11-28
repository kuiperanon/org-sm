
func TEST1(middlewareDb *MiddlewareDb) {
    // Now let's experiment with creating an "org-sm" deck, we're sending requests to the ankiconnect.
    resp, err := anki.Request("deckNamesAndIds", map[string]interface{}{})
    if err != nil {
        fmt.Printf("error in sending request\n")
        return
    }


    const NOT_FOUND = -1
    anki.MainDeckId := NOT_FOUND

    for name, idF := range resp.Result.(map[string]interface {}) {
        _id := int(idF.(float64))
        if name == anki.MainDeck {
            anki.MainDeckId = _id
            break
        }
    }

    if anki.MainDeckId == NOT_FOUND {
        // Create the deck, since it's missing.
        resp, err := anki.Request("createDeck", map[string]interface{}{"deck": anki.MainDeck})
        if err != nil {
            fmt.Printf("error in sending createDeck request\n")
            return
        }
        fmt.Printf("Deck is missing, so created deck, response to createDeck request is: %s\n", resp.Result)
    }

    // Let's get a list of all the cards in the deck.
    //

    // https://docs.ankiweb.net/searching.html
    uuid := "e31ed918-17c8-4d4a-a741-8e313ce2bb8a"
    cardId, exists, err := FindCard(uuid)
    if err != nil {
        fmt.Printf("error in checking if card exists\n")
    }
    if !exists {
        // create the missing card
        fmt.Printf("card no exist so me makin' da card\n")

        resp, err := anki.Request("addNote", map[string]interface{}{
            "note": map[string]interface{}{
                "deckName": anki.MainDeck,
                "modelName": "Basic",
                "fields": map[string]interface{}{
                    "Front": "front content",
                    "Back": "back content",
                },
                "options": map[string]interface{}{
                    "allowDuplicate": false,
                    "duplicateScope": "deck",
                    "duplicateScopeOptions": map[string]interface{}{
                        "deckName": anki.MainDeck,
                        "checkChildren": false,
                        "checkAllModels": false,
                    },
                },
                "tags": []string{"org-sm", "uuid-" + uuid},
            },
        })
        if err != nil {
            fmt.Printf("error in sending addNote request\n")
            return
        }
        fmt.Printf("added note, response is: %s\n", resp.Result)
    } else {
        fmt.Printf("found note, response is: %d\n", cardId)
    }

    // Now update the content of the card (from emacs, hypothetically)
    cardId, cardExists, err := FindCard(uuid)
    if err != nil {
        fmt.Printf("error in find card id\n")
        return
    } else if cardExists {
        resp, err := anki.Request("updateNoteFields", map[string]interface{}{
            "note": map[string]interface{}{
                "id": cardId,
                "fields": map[string]interface{}{
                    "Front": "front foobar",
                    "Back": "back foobarrs",
                },
            },
        })
        if err != nil {
            fmt.Printf("error in sending request updateNoteFields\n")
            return
        }
        fmt.Printf("update response is: %s\n", resp.Result)
    }


    // Now Get the list of items that are queued for learning today.
    elemInfos, err := middlewareDb.FindDueElements()
    if err != nil {
        fmt.Println("error in finding due elements, ", err)
        return
    }

    for _, elem := range elemInfos {
        fmt.Printf("nocheckin %+v\n", elem)
    }

    err = middlewareDb.UpdateOrInsertElementDbEntry("e31ed918-17c8-4d4a-a741-8e313ce2bb8a", ElementTypeTopic, time.Now().AddDate(0, 0, -5), 2.0, StatusEnabled)
    if err != nil {
        fmt.Println("error: ", err)
        return
    }

    // Ok now it should show up in topics
    uuids, err := FindUuidsSortedInEmacs()
    if err != nil {
        fmt.Println("error: ", err)
        return
    }

    for _, uuid := range uuids {
        elemInfo, err := FindElemInfo(uuid)
        if err != nil {
            fmt.Println("error: ", err)
            return
        }

        err = middlewareDb.UpdateOrInsertElementDbEntry(uuid, elemInfo.elementType, time.Now().AddDate(0, 0, -5), 2.0, StatusEnabled)
        if err != nil {
            fmt.Println("error: ", err)
            return
        }

        fmt.Println("uuid: ", uuid)
    }

    // TODO ok, now we wanna
}

