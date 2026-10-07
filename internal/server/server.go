package server

import (
	"Min/internal/chat"
	"Min/internal/client"
	"Min/internal/config"
	"bufio"
	"math/rand"
	"strings"
	"time"

	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
)

const dataPath = "./data"

type Server struct {
	Config *config.Config
}

func New(cfg *config.Config) *Server {
	return &Server{
		Config: cfg,
	}
}

func (s *Server) Start() {
	ch, err := chat.New(dataPath)
	if err != nil {
		fmt.Printf("Failed to initialize: %v\n", err)
		return
	}
	defer ch.Shutdown()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nReceived shutdown signal")
		ch.Shutdown()
		os.Exit(0)
	}()

	go ch.Start()

	listener, err := net.Listen(s.Config.Network, s.Config.Address)
	if err != nil {
		fmt.Println("Error starting server:", err)
		return
	}
	defer listener.Close()

	fmt.Printf("Server started on '%s' \n", s.Config.Address)

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error accepting connection:", err)
			continue
		}
		fmt.Println("New connection from:", conn.RemoteAddr())
		go s.work(conn, ch)
	}
}

func (s *Server) work(conn net.Conn, ch *chat.Chat) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Panic in handleClient: %v\n", r)
		}
		conn.Close()
	}()

	// Set initial timeout for Name entry
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	reader := bufio.NewReader(conn)

	// Prompt for Name or reconnection
	conn.Write([]byte("Enter Name (or 'reconnect:<Name>:<token>'): \n"))

	input, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println("Failed to read Name:", err)
		return
	}
	input = strings.TrimSpace(input)

	var (
		username       string
		reconnectToken string
		isReconnecting bool
	)

	// Parse reconnection attempt
	if strings.HasPrefix(input, "reconnect:") {
		parts := strings.Split(input, ":")
		if len(parts) == 3 {
			username = parts[1]
			reconnectToken = parts[2]
			isReconnecting = true
		} else {
			conn.Write([]byte("Invalid format. Use: reconnect:<Name>:<token>\n"))
			return
		}
	} else {
		username = input
	}

	// Generate guest name if empty
	if username == "" {
		username = fmt.Sprintf("Guest%d", rand.Intn(1000))
	}

	// Validate reconnection or check for duplicate
	if isReconnecting {
		if ch.IsValidToken(username, reconnectToken) {
			fmt.Printf("%s reconnected successfully\n", username)
			conn.Write([]byte(fmt.Sprintf("Welcome back, %s!\n", username)))
		} else {
			conn.Write([]byte("Invalid token or session expired.\n"))
			return
		}
	} else {
		// Prevent duplicate logins
		if ch.IsUserConnected(username) {
			conn.Write([]byte("Name already connected. Use reconnect if you lost connection.\n"))
			return
		}

		// Create or retrieve session
		ch.SessionsMu.Lock()
		existingSession := ch.Sessions[username]
		ch.SessionsMu.Unlock()

		if existingSession != nil {
			token := existingSession.Token
			msg := fmt.Sprintf("Tip: Save this token: %s\n", token)
			msg += fmt.Sprintf("To reconnect: reconnect:%s:%s\n", username, token)
			conn.Write([]byte(msg))
		} else {
			session := ch.NewSession(username)
			token := session.Token
			msg := fmt.Sprintf("Your token: %s\n", token)
			msg += fmt.Sprintf("To reconnect: reconnect:%s:%s\n", username, token)
			conn.Write([]byte(msg))
		}
	}

	// Create c object
	c := client.New(
		s.Config,
		conn,
		username,
		reconnectToken,
	)

	// Clear timeout for normal operation
	conn.SetReadDeadline(time.Time{})

	// Notify chatroom
	ch.Join <- c

	// Send welcome message
	welcomeMsg := chat.BuildWelcomeMessage(username)
	conn.Write([]byte(welcomeMsg))

	// Start read/write loops
	go chat.ReadMessages(c, ch)
	c.WriteMessages() // Blocks until disconnect

	// Update session on disconnect
	ch.UpdateSessionActivity(username)
	ch.Leave <- c
}
