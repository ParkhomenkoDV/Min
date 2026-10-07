package chat

import (
	"Min/internal/client"
	"Min/internal/message"
	"Min/internal/session"
	"Min/pkg/token"
	"strings"

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
	Join      chan *client.Client
	Leave     chan *client.Client
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
	Sessions   map[string]*session.Session
	SessionsMu sync.Mutex
}

func New(dataDir string) (*Chat, error) {
	ch := &Chat{
		clients:   make(map[*client.Client]bool),
		Join:      make(chan *client.Client),
		Leave:     make(chan *client.Client),
		broadcast: make(chan string),
		listUsers: make(chan *client.Client),
		Sessions:  make(map[string]*session.Session),
		messages:  make([]message.Message, 0),
		startTime: time.Now(),
		dataDir:   dataDir,
	}

	// Restore from snapshot if available
	if err := ch.loadSnapshot(); err != nil {
		fmt.Printf("Failed to load snapshot: %v\n", err)
	}

	// Initialize WAL for new messages
	if err := ch.initPersistence(); err != nil {
		return nil, err
	}

	// Start background snapshot work
	go ch.periodicSnapshots(5 * time.Minute)

	return ch, nil
}

func (ch *Chat) loadSnapshot() error {
	snapshotPath := filepath.Join(ch.dataDir, "snapshot.json")
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

	ch.messageMu.Lock()
	err = json.Unmarshal(data, &ch.messages)
	ch.messageMu.Unlock()

	if err != nil {
		return err
	}

	for _, msg := range ch.messages {
		if msg.ID >= ch.nextMessageID {
			ch.nextMessageID = msg.ID + 1
		}
	}

	fmt.Printf("Loaded %d messages from snapshot\n", len(ch.messages))
	return nil
}

func (ch *Chat) initPersistence() error {
	if err := os.MkdirAll(ch.dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	walPath := filepath.Join(ch.dataDir, "messages.wal")

	if err := ch.recoverFromWAL(walPath); err != nil {
		fmt.Printf("Recovery failed: %v\n", err)
	}

	file, err := os.OpenFile(walPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open wal: %w", err)
	}

	ch.walFile = file
	fmt.Printf("WAL initialized: %s\n", walPath)
	return nil
}

func (ch *Chat) periodicSnapshots(duration time.Duration) {
	ticker := time.NewTicker(duration)
	defer ticker.Stop()

	for range ticker.C {
		ch.messageMu.Lock()
		messageCount := len(ch.messages)
		ch.messageMu.Unlock()

		if messageCount > 100 { // TODO config
			if err := ch.createSnapshot(); err != nil {
				fmt.Printf("Snapshot failed: %v\n", err)
			}
		}
	}
}

func (ch *Chat) Start() {
	fmt.Println("Chat heartbeat started...")
	go ch.cleanupInactiveClients()

	for {
		select {
		case client := <-ch.Join:
			ch.join(client)
		case client := <-ch.Leave:
			ch.leave(client)
		case message := <-ch.broadcast:
			ch.handleBroadcast(message)
		case client := <-ch.listUsers:
			ch.sendUserList(client)
		}
	}
}

func (ch *Chat) Shutdown() {
	fmt.Println("Shutting down...")
	if err := ch.createSnapshot(); err != nil {
		fmt.Printf("Final snapshot failed: %v\n", err)
	}
	if ch.walFile != nil {
		ch.walFile.Close()
	}
	fmt.Println("Shutdown complete")
}

func (ch *Chat) NewSession(username string) *session.Session {
	ch.SessionsMu.Lock()
	defer ch.SessionsMu.Unlock()

	tok := token.GenerateToken()

	session := session.New(username, tok)

	ch.Sessions[username] = session

	fmt.Printf("Created session for %s (token: %s...)\n", username, tok[:8])

	return session
}

func (ch *Chat) IsValidToken(username, token string) bool {
	ch.SessionsMu.Lock()
	defer ch.SessionsMu.Unlock()

	session, exists := ch.Sessions[username]
	if !exists {
		return false
	}

	if session.Token != token {
		return false
	}

	if time.Since(session.LastSeen) > 1*time.Hour { // TODO config
		delete(ch.Sessions, username)
		return false
	}

	session.LastSeen = time.Now()

	return true
}

func (ch *Chat) UpdateSessionActivity(username string) {
	ch.SessionsMu.Lock()
	defer ch.SessionsMu.Unlock()

	if session, exists := ch.Sessions[username]; exists {
		session.LastSeen = time.Now()
	}
}

func (ch *Chat) IsUserConnected(name string) bool {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	for client := range ch.clients {
		if client.Name == name {
			return true
		}
	}

	return false
}

func (ch *Chat) cleanupInactiveClients() {
	ticker := time.NewTicker(30 * time.Second) // TODO config
	defer ticker.Stop()

	for range ticker.C {
		ch.mu.Lock()
		var toRemove []*client.Client

		for client := range ch.clients {
			if client.IsActive(5 * time.Minute) { // TODO config
				fmt.Printf("Removing inactive: %s\n", client.Name)
				toRemove = append(toRemove, client)
			}
		}
		ch.mu.Unlock()

		for _, client := range toRemove {
			ch.Leave <- client
		}
	}
}

func (ch *Chat) recoverFromWAL(walPath string) error {
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

		ch.messages = append(ch.messages, msg)

		if msg.ID >= ch.nextMessageID {
			ch.nextMessageID = msg.ID + 1
		}
		recovered++
	}

	fmt.Printf("Recovered %d messages\n", recovered)
	return nil
}

func (ch *Chat) persistMessage(msg message.Message) error {
	ch.walMu.Lock()
	defer ch.walMu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = ch.walFile.Write(append(data, '\n'))
	if err != nil {
		return err
	}

	return ch.walFile.Sync()
}

func (ch *Chat) createSnapshot() error {
	snapshotPath := filepath.Join(ch.dataDir, "snapshot.json")
	tempPath := snapshotPath + ".tmp"

	file, err := os.Create(tempPath)
	if err != nil {
		return err
	}
	defer file.Close()

	ch.messageMu.Lock()
	data, err := json.MarshalIndent(ch.messages, "", "  ")
	ch.messageMu.Unlock()

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

	fmt.Printf("Snapshot created (%d messages)\n", len(ch.messages))
	return ch.truncateWAL()
}

func (ch *Chat) truncateWAL() error {
	ch.walMu.Lock()
	defer ch.walMu.Unlock()

	if ch.walFile != nil {
		ch.walFile.Close()
	}

	walPath := filepath.Join(ch.dataDir, "messages.wal") // TODO config
	file, err := os.OpenFile(walPath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	ch.walFile = file
	fmt.Println("WAL truncated")
	return nil
}

func (chatRoom *Chat) Command(client *client.Client, command string) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return
	}

	switch parts[0] {
	case "/users":
		chatRoom.listUsers <- client
	case "/stats":
		client.Mu.Lock()
		stats := "Your Stats:\n"
		stats += fmt.Sprintf("  Messages sent: %d\n", client.Statistic.MessagesSent)
		stats += fmt.Sprintf("  Messages received: %d\n", client.Statistic.MessagesRecv)
		stats += fmt.Sprintf("  Last active: %s ago\n",
			time.Since(client.LastActive).Round(time.Second))
		client.Mu.Unlock()

		select {
		case client.Outgoing <- stats:
		default:
		}
	case "/msg":
		if len(parts) < 3 {
			select {
			case client.Outgoing <- "Usage: /msg <Name> <message>\n":
			default:
			}
			return
		}

		targetName := parts[1]
		messageText := strings.Join(parts[2:], " ")

		targetClient := chatRoom.FindClientByUsername(targetName)
		if targetClient == nil {
			select {
			case client.Outgoing <- fmt.Sprintf("User '%s' not found\n", targetName):
			default:
			}
			return
		}

		privateMsg := fmt.Sprintf("[From %s]: %s\n", client.Name, messageText)
		select {
		case targetClient.Outgoing <- privateMsg:
		default:
			select {
			case client.Outgoing <- fmt.Sprintf("%s's inbox is full\n", targetName):
			default:
			}
			return
		}

		select {
		case client.Outgoing <- fmt.Sprintf("Message sent to %s\n", targetName):
		default:
		}
	case "/history":
		count := 20
		if len(parts) > 1 {
			fmt.Sscanf(parts[1], "%d", &count)
		}
		if count > 100 {
			count = 100
		}
		chatRoom.sendHistory(client, count)
	case "/token":
		chatRoom.SessionsMu.Lock()
		session := chatRoom.Sessions[client.Name]
		chatRoom.SessionsMu.Unlock()

		if session != nil {
			msg := "Your reconnect token:\n"
			msg += fmt.Sprintf("   reconnect:%s:%s\n", client.Name, session.Token)
			select {
			case client.Outgoing <- msg:
			default:
			}
		}
	case "/quit":
		announcement := fmt.Sprintf("%s left the chat\n", client.Name)
		chatRoom.broadcast <- announcement

		select {
		case client.Outgoing <- "Goodbye!\n":
		default:
		}

		time.Sleep(100 * time.Millisecond)
		client.Conn.Close()
	default:
		select {
		case client.Outgoing <- fmt.Sprintf("Unknown: %s\n", parts[0]):
		default:
		}
	}
}
