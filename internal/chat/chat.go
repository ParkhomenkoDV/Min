package chat

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

// Chat is the central coordinator
type Chat struct {
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

func New(dataDir string) (*Chat, error) {
	c := &Chat{
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
	if err := c.loadSnapshot(); err != nil {
		fmt.Printf("Failed to load snapshot: %v\n", err)
	}

	// Initialize WAL for new messages
	if err := c.initPersistence(); err != nil {
		return nil, err
	}

	// Start background snapshot work
	go c.periodicSnapshots(5 * time.Minute)

	return c, nil
}

func (c *Chat) loadSnapshot() error {
	snapshotPath := filepath.Join(c.dataDir, "snapshot.json")
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

	c.messageMu.Lock()
	err = json.Unmarshal(data, &c.messages)
	c.messageMu.Unlock()

	if err != nil {
		return err
	}

	for _, msg := range c.messages {
		if msg.ID >= c.nextMessageID {
			c.nextMessageID = msg.ID + 1
		}
	}

	fmt.Printf("Loaded %d messages from snapshot\n", len(c.messages))
	return nil
}

func (c *Chat) initPersistence() error {
	if err := os.MkdirAll(c.dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	walPath := filepath.Join(c.dataDir, "messages.wal")

	if err := c.recoverFromWAL(walPath); err != nil {
		fmt.Printf("Recovery failed: %v\n", err)
	}

	file, err := os.OpenFile(walPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open wal: %w", err)
	}

	c.walFile = file
	fmt.Printf("WAL initialized: %s\n", walPath)
	return nil
}

func (c *Chat) periodicSnapshots(duration time.Duration) {
	ticker := time.NewTicker(duration)
	defer ticker.Stop()

	for range ticker.C {
		c.messageMu.Lock()
		messageCount := len(c.messages)
		c.messageMu.Unlock()

		if messageCount > 100 { // TODO config
			if err := c.createSnapshot(); err != nil {
				fmt.Printf("Snapshot failed: %v\n", err)
			}
		}
	}
}

func (c *Chat) Run() {
	fmt.Println("Chat heartbeat started...")
	go c.cleanupInactiveClients()

	for {
		select {
		case client := <-c.join:
			c.handleJoin(client)
		case client := <-c.leave:
			c.handleLeave(client)
		case message := <-c.broadcast:
			c.handleBroadcast(message)
		case client := <-c.listUsers:
			c.sendUserList(client)
		}
	}
}

func (c *Chat) Shutdown() {
	fmt.Println("\nShutting down...")
	if err := c.createSnapshot(); err != nil {
		fmt.Printf("Final snapshot failed: %v\n", err)
	}
	if c.walFile != nil {
		c.walFile.Close()
	}
	fmt.Println("Shutdown complete")
}

func (c *Chat) createSession(username string) *session.Session {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()

	tok := token.GenerateToken()

	session := &session.Session{
		Username:  username,
		Token:     tok,
		LastSeen:  time.Now(),
		CreatedAt: time.Now(),
	}

	c.sessions[username] = session

	fmt.Printf("Created session for %s (token: %s...)\n", username, tok[:8])

	return session
}

func (c *Chat) validateReconnectToken(username, token string) bool {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()

	session, exists := c.sessions[username]
	if !exists {
		return false
	}

	if session.Token != token {
		return false
	}

	if time.Since(session.LastSeen) > 1*time.Hour {
		delete(c.sessions, username)
		return false
	}

	session.LastSeen = time.Now()

	return true
}

func (c *Chat) updateSessionActivity(username string) {
	c.sessionsMu.Lock()
	defer c.sessionsMu.Unlock()

	if session, exists := c.sessions[username]; exists {
		session.LastSeen = time.Now()
	}
}

func (c *Chat) IsUsernameConnected(name string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	for client := range c.clients {
		if client.Name == name {
			return true
		}
	}

	return false
}

func (c *Chat) cleanupInactiveClients() {
	ticker := time.NewTicker(30 * time.Second) // TODO config
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		var toRemove []*client.Client

		for client := range c.clients {
			if client.IsActive(5 * time.Minute) { // TODO config
				fmt.Printf("Removing inactive: %s\n", client.Name)
				toRemove = append(toRemove, client)
			}
		}
		c.mu.Unlock()

		for _, client := range toRemove {
			c.leave <- client
		}
	}
}

func (c *Chat) recoverFromWAL(walPath string) error {
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

		c.messages = append(c.messages, msg)

		if msg.ID >= c.nextMessageID {
			c.nextMessageID = msg.ID + 1
		}
		recovered++
	}

	fmt.Printf("Recovered %d messages\n", recovered)
	return nil
}

func (c *Chat) persistMessage(msg message.Message) error {
	c.walMu.Lock()
	defer c.walMu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = c.walFile.Write(append(data, '\n'))
	if err != nil {
		return err
	}

	return c.walFile.Sync()
}

func (c *Chat) createSnapshot() error {
	snapshotPath := filepath.Join(c.dataDir, "snapshot.json")
	tempPath := snapshotPath + ".tmp"

	file, err := os.Create(tempPath)
	if err != nil {
		return err
	}
	defer file.Close()

	c.messageMu.Lock()
	data, err := json.MarshalIndent(c.messages, "", "  ")
	c.messageMu.Unlock()

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

	fmt.Printf("Snapshot created (%d messages)\n", len(c.messages))
	return c.truncateWAL()
}

func (c *Chat) truncateWAL() error {
	c.walMu.Lock()
	defer c.walMu.Unlock()

	if c.walFile != nil {
		c.walFile.Close()
	}

	walPath := filepath.Join(c.dataDir, "messages.wal")
	file, err := os.OpenFile(walPath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	c.walFile = file
	fmt.Println("WAL truncated")
	return nil
}
