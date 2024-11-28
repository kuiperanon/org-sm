package emacs

import (
	"fmt"
	"strconv"
	"os/exec"
	"strings"
	"sort"
	"time"
)

const (
    orgDirectory = "~/org/"
)

const (
    StatusEnabled = 1
    StatusDisabled = 2
)

const (
    ElementTypeTopic = ":topic"
    ElementTypeItem = ":item"
)

// These are the part of the ElemInfo for which the source of truth comes from MWDB and these are only considered the source of truth (when they come from emacs) if they are missing in MWDB.
type TopicInfo struct {
    LastReview time.Time
    Interval float64
}

func (t *TopicInfo) DueDate() (dueDate time.Time, err error) {
    reviewInterval, err := time.ParseDuration(fmt.Sprintf("%dh", int(t.Interval) * 24))
    if err != nil {
        return
    }
    dueDate = t.LastReview.Add(reviewInterval)
    return
}

// TODO implement content of the cards being stored too
type ElemInfo struct {
    Uuid string
    Priority int
    AFactor float64 // TODO Topic only
    ElementType string
    CardId int
    TopicInfo TopicInfo // Only set if topic
    Status int // 1 means "Dismissed"
}

var ElemInfoCache map[string]ElemInfo // Maps UUID to ElemInfo

type NilError struct {}

func (e NilError) Error() string {
	return "Result was nil"
}

type CommandResult []byte

func Command(lisp string) (CommandResult, error) {
    cmd := exec.Command("emacsclient", "-e", lisp)
	stdout, err := cmd.Output()
	if string(stdout) == "nil" {
		err = NilError{}
	}
	if err != nil {
		err = fmt.Errorf(".emacs Error executing lisp: `%s`.  Error: %v", lisp, err)
	}
	return stdout, err
}

func (r CommandResult) AsString() (string, error) {
	output := string(r)
	if output == "nil" {
		return "", NilError{}
	}
	s := string(output)
	return s[1:len(s)-2], nil
}

func (r CommandResult) AsStrings() (result []string, err error) {
    s, isNil := r.AsString()
	if isNil != nil {
		err = isNil
		return
	}
	for _, v := range strings.Split(s[1:len(s)-2], " ") {
		result = append(result, v)
	}
	return
}

func FindUuidsSorted() (sortedUuids []string, err error) {
    result, err := Command("(get-org-ids-with-tag-in-directory \"" + orgDirectory + "\" \"drill\" \"dismissed\")")
    if err != nil {
        err = fmt.Errorf("emacs.FindUuidsSorted: Error executing `get-org-ids-with-tag-in-directory` using emacsclient: %s", err.Error())
        return
    }

    uuids, isNil := result.AsStrings()
	if isNil != nil {
		err = isNil
		return
	}

    for _, v := range uuids {
        if v != "nil" && v != "n" {
			fmt.Println("nocheckin FindUuidsSorted: ", v)
            uuid := v[1:len(v)-1]
            sortedUuids = append(sortedUuids, uuid)

            // Now confirm the uuid is valid
            stdout, err := Command("(org-id-find \"" + uuid + "\" 'marker)")
			_ = stdout
            if err != nil {
                err := fmt.Errorf("error in emacs.FindUuidsSorted: %s", err.Error())
                return sortedUuids, err
            }

        }
    }

    fmt.Println("Sorting Uuids from Emacs")
    sort.Strings(sortedUuids)

    return
}

func GetPropertyString(uuid string, property string) (result string, useDefault bool, elemExists bool, err error) {
	// Use emacs to get priority
	stdoutBytes, err := Command("(org-get-property-value-by-id \"" + uuid + "\" \"" + property + "\")")
	if err != nil {
		err = fmt.Errorf("Error executing `org-get-property-value-by-id` using emacsclient: %s", err.Error())
		return
	}
	stdout := string(stdoutBytes)
	if strings.HasPrefix(stdout, "\"_I") {
		fmt.Println("WARNING: This UUID = %s is invalid.\n", uuid)
		elemExists = false
		return
	} else if strings.HasPrefix(stdout, "\"_p") {
		useDefault = true
		return "", useDefault, true, nil
	} else {
		result = stdout[1:len(stdout)-2]
		return result, false, true, nil
	}
}

func GetLastReviewDate(uuid string) (date time.Time, err error) {
    stdoutBytes, err := Command("(org-id-get-first-timestamp \"" + uuid + "\")")
    if err != nil {
        return
    }
    stdout := string(stdoutBytes)
    stdout = stdout[1:len(stdout)-2]
    if stdout[0] == 'i' {
        err = fmt.Errorf("`org-id-get-first-timestamp` command resulted in nil for uuid: %s", uuid)
		// TODO I may want to remove this since I return NillError in Command anyways, check to confirm that actually does as I expect.
        return
    }
    date, err = time.Parse("2006-01-02 15:04:05", stdout)
	return
}

func FindElemInfo(uuid string) (elemInfo ElemInfo, exists bool, err error) {
	// TODO Maybe I should make the ElemInfoCache expire. Possibly this requires being defined in the RequiresCacheUpdate function
    elemInfo, exists = ElemInfoCache[uuid]
    if !exists || elemInfo.RequiresCacheUpdate() {
        elemInfo, exists, err = UpdateElemInfoCache(uuid)
    }
	return
}

func UpdateElemInfoCache(uuid string) (elemInfo ElemInfo, exists bool, err error) {
    // TODO nocheckin : I need to also check if the emacs uuids are dismissed, or DONE!
    priorityString, useDefault, exists, err := GetPropertyString(uuid, "SM_PRIORITY")
    if err != nil || !exists {
        return
    } else if useDefault {
        elemInfo.Priority = 66
    } else {
        priority, err := strconv.ParseFloat(priorityString, 64)
        if err != nil {
            fmt.Println("error with uuid: ", uuid, ", error =", err.Error())
            return elemInfo, exists, err
        }
        elemInfo.Priority = int(priority)
    }

    afactorString, useDefault, exists, err := GetPropertyString(uuid, "SM_A_FACTOR")
    if err != nil || !exists {
        return
    } else if useDefault {
        elemInfo.AFactor = 1.2
    } else {
        elemInfo.AFactor, err = strconv.ParseFloat(afactorString, 64)
        if err != nil {
            fmt.Println("error with uuid: ", uuid, ", error =", err.Error())
            return elemInfo, exists, err
        }
    }

    elemInfo.ElementType, useDefault, exists, err = GetPropertyString(uuid, "SM_ELEMENT_TYPE")
    if err != nil || !exists {
        return
    } else if useDefault {
        err = fmt.Errorf("error: Emacs element (uuid=%s) missing elementType.\n", uuid)
        return
    }
	if !elemInfo.IsElementTypeValid() {
		err = fmt.Errorf("error: elementType for ID (%s) is invalid. elementType = %s\n", uuid, elemInfo.ElementType)
		return
	}

	// Emacs is not the source of truth for lastReview. MWDB is the source truth for lastReview. However, this is useful in case that info is missing from MWDB.
    elemInfo.TopicInfo.LastReview, err = GetLastReviewDate(uuid)
    if err != nil {
        fmt.Println("Could not parse a date at org element with uuid: ", uuid)
        return
    }

    elemInfo.TopicInfo.Interval = 3.0 // TODO nocheckin : decide how to handle this

    elemInfo.Uuid = uuid

	elemInfo.Status = StatusEnabled // TODO nocheckin : do i need to check this with org/emacs, if the :dismissed tag is there?

	ElemInfoCache[uuid] = elemInfo
    return
}

func (ei *ElemInfo) UpdateTopicInfo() {
	// This function is called whenever you complete a topic
	ei.TopicInfo.Interval = ei.TopicInfo.Interval * ei.AFactor
	ei.TopicInfo.LastReview = time.Now()
}

func (elemInfo *ElemInfo) RequiresCacheUpdate() bool {
    // needs update
    // TODO  use timestamps
    return false
}

func (ei *ElemInfo) IsDismissed() bool {
    return ei.Status == StatusDisabled // TODO nocheckin use an enum for this or constant
}

func (ei *ElemInfo) IsTopic() bool {
    return ei.ElementType == ElementTypeTopic
}

func (ei *ElemInfo) IsItem() bool {
    return ei.ElementType == ElementTypeItem
}

func (ei *ElemInfo) IsElementTypeValid() bool {
    return ei.IsTopic() || ei.IsItem()
}
