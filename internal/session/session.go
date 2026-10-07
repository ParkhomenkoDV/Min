package session

import (
	"time"
)

// Session tracks reconnection data
type Session struct {
	Name      string
	Token     string
	LastSeen  time.Time
	CreatedAt time.Time
}

func New(username, token string) *Session {
	return &Session{
		Name:      username,
		Token:     token,
		LastSeen:  time.Now(),
		CreatedAt: time.Now(),
	}
}
