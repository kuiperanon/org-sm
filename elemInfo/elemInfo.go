package elemInfo

import (
	"fmt"
	"time"
	"log"
)

const (
    StatusEnabled = 1
    StatusDisabled = 2
)

const (
    ElementTypeTopic = ":topic"
    ElementTypeItem = ":item"
)

// TODO explain why these are "topic info" -- the rest of it is handled by anki
type TopicInfo struct {
    LastReview time.Time
    Interval float64
}

func (t *ElemInfo) DueDate() (dueDate time.Time, err error) {
	dueDate, err = t.TopicInfo.DueDate()
    return
}

func (t *TopicInfo) DueDate() (dueDate time.Time, err error) {
    reviewInterval, err := time.ParseDuration(fmt.Sprintf("%dh", int(t.Interval) * 24))
    if err != nil {
        return
    }
    dueDate = t.LastReview.Add(reviewInterval)
	fmt.Println("t.LastReview=", t.LastReview, ", reviewInterval=", reviewInterval)
	return
}

// TODO implement content of the cards being stored too
type ElemInfo struct {
    Uuid string
    Priority int// TODO why not float?
    AFactor float64 // TODO Topic only
    ElementType string
    TopicInfo TopicInfo // Only set if topic
    Status int // 1 means "Dismissed"
}

func Default() ElemInfo {
	var ei ElemInfo
	ei.Uuid = ""
	ei.Priority = 66
	ei.AFactor = 1.2
	ei.ElementType = ElementTypeTopic
	ei.TopicInfo.LastReview = time.Now()
    ei.TopicInfo.Interval = 1
    ei.Status = StatusEnabled
	return ei
}

func (ei *ElemInfo) UpdateTopicInfo() {
	// This function is called whenever you complete a review/repetition of a topic
	ei.TopicInfo.Interval = ei.TopicInfo.Interval * ei.AFactor
	if ei.TopicInfo.Interval < 0.5 { // TODO nocheckin :: This was an arbitrary decision--to avoid bugs where interval is 0
		ei.TopicInfo.Interval = 0.5
	}
	ei.TopicInfo.LastReview = time.Now()
}

func (elemInfo *ElemInfo) RequiresCacheUpdate() bool {
	// nocheckin :: Remove?
    // needs update
    // TODO  use timestamps
    return false
}

func (ei ElemInfo) IsDueToday() bool {
	dueDate, err := ei.DueDate()
	if err != nil {
		log.Fatal("Failed to get DueDate for ei, err =", err)
	}
	return dueDate.Compare(time.Now()) <= 0
}

func (ei *ElemInfo) IsDismissed() bool {
    return ei.Status == StatusDisabled
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
