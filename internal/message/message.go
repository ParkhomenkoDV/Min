package message

import (
	"Min/internal/client"
	"time"
)

// Message represents a single chat message with metadata
type Message struct {
	ID        uint64    `json:"id"`
	From      string    `json:"from"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Channel   string    `json:"channel"` // "global" or "private:username"
}

// DirectMessage represents a private message
type DirectMessage struct {
	ToClient *client.Client
	Message  string
}
