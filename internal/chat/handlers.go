package chat

import (
	"Min/internal/client"
	"Min/internal/message"
	"bufio"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"time"
)

func Run(conn net.Conn, c *Chat) {
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

	var username string
	var reconnectToken string
	var isReconnecting bool

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
		if c.validateReconnectToken(username, reconnectToken) {
			fmt.Printf("%s reconnected successfully\n", username)
			conn.Write([]byte(fmt.Sprintf("Welcome back, %s!\n", username)))
		} else {
			conn.Write([]byte("Invalid token or session expired.\n"))
			return
		}
	} else {
		// Prevent duplicate logins
		if c.IsUsernameConnected(username) {
			conn.Write([]byte("Name already connected. Use reconnect if you lost connection.\n"))
			return
		}

		// Create or retrieve session
		c.sessionsMu.Lock()
		existingSession := c.sessions[username]
		c.sessionsMu.Unlock()

		if existingSession != nil {
			token := existingSession.Token
			msg := fmt.Sprintf("Tip: Save this token: %s\n", token)
			msg += fmt.Sprintf("To reconnect: reconnect:%s:%s\n", username, token)
			conn.Write([]byte(msg))
		} else {
			session := c.createSession(username)
			token := session.Token
			msg := fmt.Sprintf("Your token: %s\n", token)
			msg += fmt.Sprintf("To reconnect: reconnect:%s:%s\n", username, token)
			conn.Write([]byte(msg))
		}
	}

	// Create client object
	client := &client.Client{
		Conn:       conn,
		Name:       username,
		Outgoing:   make(chan string, 10), // Buffered
		LastActive: time.Now(),
		Token:      reconnectToken,
	}

	// Clear timeout for normal operation
	conn.SetReadDeadline(time.Time{})

	// Notify chatroom
	c.join <- client

	// Send welcome message
	welcomeMsg := buildWelcomeMessage(username)
	conn.Write([]byte(welcomeMsg))

	// Start read/write loops
	go readMessages(client, c)
	writeMessages(client) // Blocks until disconnect

	// Update session on disconnect
	c.updateSessionActivity(username)
	c.leave <- client
}

func buildWelcomeMessage(name string) string {
	msg := fmt.Sprintf("Welcome, %s!\n", name)
	msg += "Commands:\n"
	msg += "  /users - List all users\n"
	msg += "  /history [N] - Show last N messages\n"
	msg += "  /msg <user> <msg> - Private message\n"
	msg += "  /token - Show your reconnect token\n"
	msg += "  /stats - Show your stats\n"
	msg += "  /quit - Leave\n"
	return msg
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

func handleCommand(client *client.Client, chatRoom *Chat, command string) {
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
		chatRoom.sessionsMu.Lock()
		session := chatRoom.sessions[client.Name]
		chatRoom.sessionsMu.Unlock()

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

func readMessages(client *client.Client, chatRoom *Chat) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Panic in readMessages for %s: %v\n", client.Name, r)
		}
	}()

	reader := bufio.NewReader(client.Conn)

	for {
		// Set 5-minute idle timeout
		client.Conn.SetReadDeadline(time.Now().Add(5 * time.Minute))

		message, err := reader.ReadString('\n')
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				fmt.Printf("%s timed out\n", client.Name)
			} else {
				fmt.Printf("%s disconnected: %v\n", client.Name, err)
			}
			return
		}

		client.SetActive() // Update activity timestamp

		message = strings.TrimSpace(message)
		if message == "" {
			continue
		}

		client.Mu.Lock()
		client.Statistic.MessagesRecv++
		client.Mu.Unlock()

		// Process commands vs. regular messages
		if strings.HasPrefix(message, "/") {
			handleCommand(client, chatRoom, message)
			continue
		}

		// Regular message - format and broadcast
		formatted := fmt.Sprintf("[%s]: %s\n", client.Name, message)
		chatRoom.broadcast <- formatted
	}
}

func (c *Chat) handleLeave(client *client.Client) {
	c.mu.Lock()
	if !c.clients[client] {
		c.mu.Unlock()
		return
	}
	delete(c.clients, client)
	c.mu.Unlock()

	fmt.Printf("%s left (total: %d)\n", client.Name, len(c.clients))

	// Close channel safely
	select {
	case <-client.Outgoing:
		// Already closed
	default:
		close(client.Outgoing)
	}

	announcement := fmt.Sprintf("*** %s left the chat ***\n", client.Name)
	c.handleBroadcast(announcement)
}

func (c *Chat) sendHistory(client *client.Client, count int) {
	c.messageMu.Lock()
	defer c.messageMu.Unlock()

	start := len(c.messages) - count
	if start < 0 {
		start = 0
	}

	historyMsg := "Recent messages:\n"
	for i := start; i < len(c.messages); i++ {
		msg := c.messages[i]
		historyMsg += fmt.Sprintf(" [%s]: %s\n", msg.From, msg.Content)
	}

	select {
	case client.Outgoing <- historyMsg:
	default:
	}
}

func (c *Chat) sendUserList(client *client.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()

	list := "Users online:\n"
	for c := range c.clients {
		status := ""
		if c.IsActive(1 * time.Minute) {
			status = " (idle)"
		}
		list += fmt.Sprintf("  - %s%s\n", c.Name, status)
	}

	list += fmt.Sprintf("\nTotal messages: %d\n", c.totalMessages)
	list += fmt.Sprintf("Uptime: %s\n", time.Since(c.startTime).Round(time.Second))

	select {
	case client.Outgoing <- list:
	default:
	}
}

func (c *Chat) FindClientByUsername(username string) *client.Client {
	c.mu.Lock()
	defer c.mu.Unlock()

	for client := range c.clients {
		if client.Name == username {
			return client
		}
	}
	return nil
}

func (c *Chat) handleJoin(client *client.Client) {
	c.mu.Lock()
	c.clients[client] = true
	c.mu.Unlock()

	client.SetActive()

	fmt.Printf("%s joined (total: %d)\n", client.Name, len(c.clients))

	c.sendHistory(client, 10)

	announcement := fmt.Sprintf("*** %s joined the chat ***\n", client.Name)
	c.handleBroadcast(announcement)
}

func (c *Chat) handleBroadcast(sms string) {
	// Parse message metadata
	parts := strings.SplitN(sms, ": ", 2)
	from := "system"
	actualContent := sms

	if len(parts) == 2 {
		from = strings.Trim(parts[0], "[]")
		actualContent = parts[1]
	}

	// Create persistent message record
	c.messageMu.Lock()
	msg := message.Message{
		ID:        c.nextMessageID,
		From:      from,
		Content:   actualContent,
		Timestamp: time.Now(),
	}
	c.nextMessageID++
	c.messages = append(c.messages, msg)
	c.messageMu.Unlock()

	// Persist to WAL
	if err := c.persistMessage(msg); err != nil {
		fmt.Printf("Failed to persist: %v\n", err)
		// Continue anyway - availability over consistency
	}

	// Collect current clients
	c.mu.Lock()
	clients := make([]*client.Client, 0, len(c.clients))
	for client := range c.clients {
		clients = append(clients, client)
	}
	c.totalMessages++
	c.mu.Unlock()

	fmt.Printf("Broadcasting to %d clients: %s", len(clients), sms)

	// Fan-out to all clients
	for _, client := range clients {
		select {
		case client.Outgoing <- sms:
			client.Mu.Lock()
			client.Statistic.MessagesSent++
			client.Mu.Unlock()
		default:
			fmt.Printf("Skipped %s (channel full)\n", client.Name)
		}
	}
}
