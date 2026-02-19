package chatroom

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func NewChatRoom(dataDir string) (*ChatRoom, error) {
	cr := &ChatRoom{
		clients:       make(map[*Client]bool),
		join:          make(chan *Client),
		leave:         make(chan *Client),
		broadcast:     make(chan string),
		listUsers:     make(chan *Client),
		directMessage: make(chan DirectMessage),
		sessions:      make(map[string]*SessionInfo),
		messages:      make([]Message, 0),
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

func runServer() {
	chatRoom, err := NewChatRoom("./data")
	if err != nil {
		fmt.Printf("Failed to initialize: %v\n", err)
		return
	}
	defer chatRoom.shutdown()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nReceived shutdown signal")
		chatRoom.shutdown()
		os.Exit(0)
	}()

	go chatRoom.Run()

	listener, err := net.Listen("tcp", ":9000")
	if err != nil {
		fmt.Println("Error starting server:", err)
		return
	}
	defer listener.Close()

	fmt.Println("Server started on :9000")

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error accepting connection:", err)
			continue
		}
		fmt.Println("New connection from:", conn.RemoteAddr())
		go handleClient(conn, chatRoom)
	}
}

func (cr *ChatRoom) shutdown() {
	fmt.Println("\nShutting down...")
	if err := cr.createSnapshot(); err != nil {
		fmt.Printf("Final snapshot failed: %v\n", err)
	}
	if cr.walFile != nil {
		cr.walFile.Close()
	}
	fmt.Println("Shutdown complete")
}
