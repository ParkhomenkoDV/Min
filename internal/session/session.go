package session

import (
	"time"
)

// Session tracks reconnection data
type Session struct {
	Username       string
	ReconnectToken string
	LastSeen       time.Time
	CreatedAt      time.Time
}
