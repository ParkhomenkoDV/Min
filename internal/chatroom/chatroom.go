package chatroom

import (
	"Min/internal/client"
	"Min/internal/message"
	"fmt"
	"os"
	"sync"
	"time"
)

// ChatRoom is the central coordinator
type ChatRoom struct {
	// Communication channels
	join          chan *client.Client
	leave         chan *client.Client
	broadcast     chan string
	listUsers     chan *client.Client
	directMessage chan message.DirectMessage

	// State
	clients       map[*client.Client]bool
	mu            sync.Mutex
	totalMessages int
	startTime     time.Time

	// Message history
	messages      []message.Message
	messageMu     sync.Mutex
	nextMessageID uint64

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

func New(dataDir string) (*ChatRoom, error) {
	cr := &ChatRoom{
		clients:       make(map[*client.Client]bool),
		join:          make(chan *client.Client),
		leave:         make(chan *client.Client),
		broadcast:     make(chan string),
		listUsers:     make(chan *client.Client),
		directMessage: make(chan message.DirectMessage),
		sessions:      make(map[string]*SessionInfo),
		messages:      make([]message.Message, 0),
		startTime:     time.Now(),
		dataDir:       dataDir,
	}

	// Restore from snapshot if available
	if err := cr.loadSnapshot(); err != nil {
		fmt.Printf("Failed to load snapshot: %v\n", err)
	}

	// Initialize WAL for new messages
	if err := cr.initializePersistence(); err != nil {
		return nil, err
	}

	// Start background snapshot worker
	go cr.periodicSnapshots()

	return cr, nil
}

func (cr *ChatRoom) periodicSnapshots() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cr.messageMu.Lock()
		messageCount := len(cr.messages)
		cr.messageMu.Unlock()

		if messageCount > 100 {
			if err := cr.createSnapshot(); err != nil {
				fmt.Printf("Snapshot failed: %v\n", err)
			}
		}
	}
}

func (cr *ChatRoom) Run() {
	fmt.Println("ChatRoom heartbeat started...")
	go cr.cleanupInactiveClients()

	for {
		select {
		case client := <-cr.join:
			cr.handleJoin(client)

		case client := <-cr.leave:
			cr.handleLeave(client)

		case message := <-cr.broadcast:
			cr.handleBroadcast(message)

		case client := <-cr.listUsers:
			cr.sendUserList(client)

		case dm := <-cr.directMessage:
			cr.handleDirectMessage(dm)
		}
	}
}

func (cr *ChatRoom) Shutdown() {
	fmt.Println("\nShutting down...")
	if err := cr.createSnapshot(); err != nil {
		fmt.Printf("Final snapshot failed: %v\n", err)
	}
	if cr.walFile != nil {
		cr.walFile.Close()
	}
	fmt.Println("Shutdown complete")
}
