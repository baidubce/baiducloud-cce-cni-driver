package cm

import (
	"fmt"
	"time"
)

// DelayEvent The controller delays the event. When the controller encounters this event,
// it will enable the delay queue to assist some tasks that need to be processed
type DelayEvent struct {
	Key      interface{}
	Duration time.Duration
	Reason   string
}

func NewDelayEvent(key interface{}, duration time.Duration, reason string) *DelayEvent {
	return &DelayEvent{
		Key:      key,
		Duration: duration,
		Reason:   reason,
	}
}

func (event *DelayEvent) String() string {
	return fmt.Sprintf("delay(%v) by %s ", event.Duration, event.Reason)
}

func (event *DelayEvent) Error() string {
	return event.String()
}
