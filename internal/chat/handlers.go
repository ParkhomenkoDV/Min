package chat

import (
	"Min/internal/client"
	"Min/internal/message"
	"bufio"
	"fmt"
	"net"
	"strings"
	"time"
)

func BuildWelcomeMessage(name string) string {
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

func ReadMessages(client *client.Client, chatRoom *Chat) {
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
			chatRoom.Command(client, message)
			continue
		}

		// Regular message - format and broadcast
		formatted := fmt.Sprintf("[%s]: %s\n", client.Name, message)
		chatRoom.broadcast <- formatted
	}
}

func (ch *Chat) leave(client *client.Client) {
	ch.mu.Lock()
	if !ch.clients[client] {
		ch.mu.Unlock()
		return
	}
	delete(ch.clients, client)
	ch.mu.Unlock()

	fmt.Printf("%s left (total: %d)\n", client.Name, len(ch.clients))

	// Close channel safely
	select {
	case <-client.Outgoing:
		// Already closed
	default:
		close(client.Outgoing)
	}

	announcement := fmt.Sprintf("*** %s left the chat ***\n", client.Name)
	ch.handleBroadcast(announcement)
}

func (ch *Chat) sendHistory(client *client.Client, count int) {
	ch.messageMu.Lock()
	defer ch.messageMu.Unlock()

	start := len(ch.messages) - count
	if start < 0 {
		start = 0
	}

	historyMsg := "Recent messages:\n"
	for i := start; i < len(ch.messages); i++ {
		msg := ch.messages[i]
		historyMsg += fmt.Sprintf(" [%s]: %s\n", msg.From, msg.Content)
	}

	select {
	case client.Outgoing <- historyMsg:
	default:
	}
}

func (ch *Chat) sendUserList(client *client.Client) {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	list := "Users online:\n"
	for c := range ch.clients {
		status := ""
		if c.IsActive(1 * time.Minute) {
			status = " (idle)"
		}
		list += fmt.Sprintf("  - %s%s\n", c.Name, status)
	}

	list += fmt.Sprintf("\nTotal messages: %d\n", ch.totalMessages)
	list += fmt.Sprintf("Uptime: %s\n", time.Since(ch.startTime).Round(time.Second))

	select {
	case client.Outgoing <- list:
	default:
	}
}

func (ch *Chat) FindClientByUsername(username string) *client.Client {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	for client := range ch.clients {
		if client.Name == username {
			return client
		}
	}
	return nil
}

func (ch *Chat) join(client *client.Client) {
	ch.mu.Lock()
	ch.clients[client] = true
	ch.mu.Unlock()

	client.SetActive()

	fmt.Printf("%s joined (total: %d)\n", client.Name, len(ch.clients))

	ch.sendHistory(client, 10)

	announcement := fmt.Sprintf("*** %s joined the chat ***\n", client.Name)
	ch.handleBroadcast(announcement)
}

func (ch *Chat) handleBroadcast(sms string) {
	// Parse message metadata
	parts := strings.SplitN(sms, ": ", 2)
	from := "system"
	actualContent := sms

	if len(parts) == 2 {
		from = strings.Trim(parts[0], "[]")
		actualContent = parts[1]
	}

	// Create persistent message record
	ch.messageMu.Lock()
	msg := message.Message{
		ID:        ch.nextMessageID,
		From:      from,
		Content:   actualContent,
		Timestamp: time.Now(),
	}
	ch.nextMessageID++
	ch.messages = append(ch.messages, msg)
	ch.messageMu.Unlock()

	// Persist to WAL
	if err := ch.persistMessage(msg); err != nil {
		fmt.Printf("Failed to persist: %v\n", err)
		// Continue anyway - availability over consistency
	}

	// Collect current clients
	ch.mu.Lock()
	clients := make([]*client.Client, 0, len(ch.clients))
	for client := range ch.clients {
		clients = append(clients, client)
	}
	ch.totalMessages++
	ch.mu.Unlock()

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
