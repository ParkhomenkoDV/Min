package chatroom

import (
	"Min/internal/client"
	"Min/internal/message"
	"Min/internal/session"
	"Min/pkg/token"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ChatRoom is the central coordinator
type ChatRoom struct {
	// Communication channels
	join      chan *client.Client
	leave     chan *client.Client
	broadcast chan string
	listUsers chan *client.Client

	// State
	clients       map[*client.Client]bool
	mu            sync.Mutex
	totalMessages uint64
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
	sessions   map[string]*session.Session
	sessionsMu sync.Mutex
}

func New(dataDir string) (*ChatRoom, error) {
	cr := &ChatRoom{
		clients:   make(map[*client.Client]bool),
		join:      make(chan *client.Client),
		leave:     make(chan *client.Client),
		broadcast: make(chan string),
		listUsers: make(chan *client.Client),
		sessions:  make(map[string]*session.Session),
		messages:  make([]message.Message, 0),
		startTime: time.Now(),
		dataDir:   dataDir,
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
	go cr.periodicSnapshots(5 * time.Minute)

	return cr, nil
}

func (cr *ChatRoom) periodicSnapshots(duration time.Duration) {
	ticker := time.NewTicker(duration)
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

func (cr *ChatRoom) createSession(username string) *session.Session {
	cr.sessionsMu.Lock()
	defer cr.sessionsMu.Unlock()

	tok := token.GenerateToken()

	session := &session.Session{
		Username:       username,
		ReconnectToken: tok,
		LastSeen:       time.Now(),
		CreatedAt:      time.Now(),
	}

	cr.sessions[username] = session

	fmt.Printf("Created session for %s (token: %s...)\n", username, tok[:8])

	return session
}

func (cr *ChatRoom) validateReconnectToken(username, token string) bool {
	cr.sessionsMu.Lock()
	defer cr.sessionsMu.Unlock()

	session, exists := cr.sessions[username]
	if !exists {
		return false
	}

	if session.ReconnectToken != token {
		return false
	}

	if time.Since(session.LastSeen) > 1*time.Hour {
		delete(cr.sessions, username)
		return false
	}

	session.LastSeen = time.Now()

	return true
}

func (cr *ChatRoom) updateSessionActivity(username string) {
	cr.sessionsMu.Lock()
	defer cr.sessionsMu.Unlock()

	if session, exists := cr.sessions[username]; exists {
		session.LastSeen = time.Now()
	}
}

func (cr *ChatRoom) IsUsernameConnected(name string) bool {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	for client := range cr.clients {
		if client.Name == name {
			return true
		}
	}

	return false
}

func (cr *ChatRoom) cleanupInactiveClients() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		cr.mu.Lock()
		var toRemove []*client.Client

		for client := range cr.clients {
			if client.IsActive(5 * time.Minute) {
				fmt.Printf("Removing inactive: %s\n", client.Name)
				toRemove = append(toRemove, client)
			}
		}
		cr.mu.Unlock()

		for _, client := range toRemove {
			cr.leave <- client
		}
	}
}

func (cr *ChatRoom) initializePersistence() error {
	if err := os.MkdirAll(cr.dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	walPath := filepath.Join(cr.dataDir, "messages.wal")

	if err := cr.recoverFromWAL(walPath); err != nil {
		fmt.Printf("Recovery failed: %v\n", err)
	}

	file, err := os.OpenFile(walPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open wal: %w", err)
	}

	cr.walFile = file
	fmt.Printf("WAL initialized: %s\n", walPath)
	return nil
}

func (cr *ChatRoom) recoverFromWAL(walPath string) error {
	file, err := os.Open(walPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No WAL found (fresh start)")
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	recovered := 0

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var msg message.Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			fmt.Printf("Skipping corrupt line: %s\n", line)
			continue
		}

		cr.messages = append(cr.messages, msg)

		if msg.ID >= cr.nextMessageID {
			cr.nextMessageID = msg.ID + 1
		}
		recovered++
	}

	fmt.Printf("Recovered %d messages\n", recovered)
	return nil
}

func (cr *ChatRoom) persistMessage(msg message.Message) error {
	cr.walMu.Lock()
	defer cr.walMu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = cr.walFile.Write(append(data, '\n'))
	if err != nil {
		return err
	}

	return cr.walFile.Sync()
}

func (cr *ChatRoom) createSnapshot() error {
	snapshotPath := filepath.Join(cr.dataDir, "snapshot.json")
	tempPath := snapshotPath + ".tmp"

	file, err := os.Create(tempPath)
	if err != nil {
		return err
	}
	defer file.Close()

	cr.messageMu.Lock()
	data, err := json.MarshalIndent(cr.messages, "", "  ")
	cr.messageMu.Unlock()

	if err != nil {
		return err
	}

	if _, err := file.Write(data); err != nil {
		return err
	}

	if err := file.Sync(); err != nil {
		return err
	}

	file.Close()

	if err := os.Rename(tempPath, snapshotPath); err != nil {
		return err
	}

	fmt.Printf("Snapshot created (%d messages)\n", len(cr.messages))
	return cr.truncateWAL()
}

func (cr *ChatRoom) truncateWAL() error {
	cr.walMu.Lock()
	defer cr.walMu.Unlock()

	if cr.walFile != nil {
		cr.walFile.Close()
	}

	walPath := filepath.Join(cr.dataDir, "messages.wal")
	file, err := os.OpenFile(walPath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	cr.walFile = file
	fmt.Println("WAL truncated")
	return nil
}

func (cr *ChatRoom) loadSnapshot() error {
	snapshotPath := filepath.Join(cr.dataDir, "snapshot.json")
	file, err := os.Open(snapshotPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return err
	}

	cr.messageMu.Lock()
	err = json.Unmarshal(data, &cr.messages)
	cr.messageMu.Unlock()

	if err != nil {
		return err
	}

	for _, msg := range cr.messages {
		if msg.ID >= cr.nextMessageID {
			cr.nextMessageID = msg.ID + 1
		}
	}

	fmt.Printf("Loaded %d messages from snapshot\n", len(cr.messages))
	return nil
}
