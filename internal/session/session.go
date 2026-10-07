package session

import (
	"time"
)

// Session tracks reconnection data
type Session struct {
	Username  string
	Token     string
	LastSeen  time.Time
	CreatedAt time.Time
}
