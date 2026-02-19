package chatroom

import (
	"net"
	"os"
	"sync"
	"time"
)

// Message represents a single chat message with metadata
type Message struct {
	ID        int       `json:"id"`
	From      string    `json:"from"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Channel   string    `json:"channel"` // "global" or "private:username"
}

// Client represents a connected user
type Client struct {
	conn         net.Conn    // TCP connection
	username     string      // Display name
	outgoing     chan string // Buffered channel for writes
	lastActive   time.Time   // For idle detection
	messagesSent int         // Statistics
	messagesRecv int
	isSlowClient bool // Testing flag

	reconnectToken string
	mu             sync.Mutex // Protects stats fields
}

// ChatRoom is the central coordinator
type ChatRoom struct {
	// Communication channels
	join          chan *Client
	leave         chan *Client
	broadcast     chan string
	listUsers     chan *Client
	directMessage chan DirectMessage

	// State
	clients       map[*Client]bool
	mu            sync.Mutex
	totalMessages int
	startTime     time.Time

	// Message history
	messages      []Message
	messageMu     sync.Mutex
	nextMessageID int

	// Persistence
	walFile *os.File
	walMu   sync.Mutex
	dataDir string

	// Sessions
	sessions   map[string]*SessionInfo
	sessionsMu sync.Mutex
}

// SessionInfo tracks reconnection data
type SessionInfo struct {
	Username       string
	ReconnectToken string
	LastSeen       time.Time
	CreatedAt      time.Time
}

// DirectMessage represents a private message
type DirectMessage struct {
	toClient *Client
	message  string
}
