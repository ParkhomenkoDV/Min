package message

import (
	"time"
)

// Message represents a single chat message with metadata
type Message struct {
	ID        uint64    `json:"id"`
	From      string    `json:"from"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}
