package chatroom

import (
	"Min/internal/client"
	"bufio"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"time"
)

func HandleClient(conn net.Conn, chatRoom *ChatRoom) {
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

	var Name string
	var reconnectToken string
	var isReconnecting bool

	// Parse reconnection attempt
	if strings.HasPrefix(input, "reconnect:") {
		parts := strings.Split(input, ":")
		if len(parts) == 3 {
			Name = parts[1]
			reconnectToken = parts[2]
			isReconnecting = true
		} else {
			conn.Write([]byte("Invalid format. Use: reconnect:<Name>:<token>\n"))
			return
		}
	} else {
		Name = input
	}

	// Generate guest name if empty
	if Name == "" {
		Name = fmt.Sprintf("Guest%d", rand.Intn(1000))
	}

	// Validate reconnection or check for duplicate
	if isReconnecting {
		if chatRoom.validateReconnectToken(Name, reconnectToken) {
			fmt.Printf("%s reconnected successfully\n", Name)
			conn.Write([]byte(fmt.Sprintf("Welcome back, %s!\n", Name)))
		} else {
			conn.Write([]byte("Invalid token or session expired.\n"))
			return
		}
	} else {
		// Prevent duplicate logins
		if chatRoom.IsUsernameConnected(Name) {
			conn.Write([]byte("Name already connected. Use reconnect if you lost connection.\n"))
			return
		}

		// Create or retrieve session
		chatRoom.sessionsMu.Lock()
		existingSession := chatRoom.sessions[Name]
		chatRoom.sessionsMu.Unlock()

		if existingSession != nil {
			token := existingSession.ReconnectToken
			msg := fmt.Sprintf("Tip: Save this token: %s\n", token)
			msg += fmt.Sprintf("To reconnect: reconnect:%s:%s\n", Name, token)
			conn.Write([]byte(msg))
		} else {
			session := chatRoom.createSession(Name)
			token := session.ReconnectToken
			msg := fmt.Sprintf("Your token: %s\n", token)
			msg += fmt.Sprintf("To reconnect: reconnect:%s:%s\n", Name, token)
			conn.Write([]byte(msg))
		}
	}

	// Create client object
	client := &client.Client{
		Conn:           conn,
		Name:           Name,
		Outgoing:       make(chan string, 10), // Buffered
		LastActive:     time.Now(),
		ReconnectToken: reconnectToken,
	}

	// Clear timeout for normal operation
	conn.SetReadDeadline(time.Time{})

	// Notify chatroom
	chatRoom.join <- client

	// Send welcome message
	welcomeMsg := buildWelcomeMessage(Name)
	conn.Write([]byte(welcomeMsg))

	// Start read/write loops
	go readMessages(client, chatRoom)
	writeMessages(client) // Blocks until disconnect

	// Update session on disconnect
	chatRoom.updateSessionActivity(Name)
	chatRoom.leave <- client
}

func buildWelcomeMessage(Name string) string {
	msg := fmt.Sprintf("Welcome, %s!\n", Name)
	msg += "Commands:\n"
	msg += "  /users - List all users\n"
	msg += "  /history [N] - Show last N messages\n"
	msg += "  /msg <user> <msg> - Private message\n"
	msg += "  /token - Show your reconnect token\n"
	msg += "  /stats - Show your stats\n"
	msg += "  /quit - Leave\n"
	return msg
}

func handleCommand(client *client.Client, chatRoom *ChatRoom, command string) {
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
		stats += fmt.Sprintf("  Messages sent: %d\n", client.MessagesSent)
		stats += fmt.Sprintf("  Messages received: %d\n", client.MessagesRecv)
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
		chatRoom.sessionsMu.Lock()
		session := chatRoom.sessions[client.Name]
		chatRoom.sessionsMu.Unlock()

		if session != nil {
			msg := "Your reconnect token:\n"
			msg += fmt.Sprintf("   reconnect:%s:%s\n", client.Name, session.ReconnectToken)
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

func writeMessages(client *client.Client) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Panic in writeMessages for %s: %v\n", client.Name, r)
		}
	}()

	writer := bufio.NewWriter(client.Conn)

	for message := range client.Outgoing {
		_, err := writer.WriteString(message)
		if err != nil {
			fmt.Printf("Write error for %s: %v\n", client.Name, err)
			return
		}

		err = writer.Flush()
		if err != nil {
			fmt.Printf("Flush error for %s: %v\n", client.Name, err)
			return
		}
	}
}
