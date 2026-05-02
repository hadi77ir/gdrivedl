package gdrivedl

import (
	"strings"
	"time"
)

type Event struct {
	Timestamp time.Time      `json:"timestamp"`
	Type      string         `json:"type"`
	Name      string         `json:"name"`
	Fields    map[string]any `json:"fields,omitempty"`
}

type EventObserver func(Event)

func newEvent(eventType, name string, fields map[string]any) Event {
	if len(fields) == 0 {
		fields = nil
	}
	return Event{
		Timestamp: time.Now().UTC(),
		Type:      eventType,
		Name:      name,
		Fields:    fields,
	}
}

func trimStructuredMessage(value string) string {
	return strings.TrimRight(value, "\r\n")
}
