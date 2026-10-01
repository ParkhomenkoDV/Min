package chatroom

import (
	"Min/internal/client"
	"Min/internal/message"
	"bufio"
	"fmt"
	"net"
	"strings"
	"time"
)

func readMessages(client *client.Client, chatRoom *ChatRoom) {
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

		client.MarkActive() // Update activity timestamp

		message = strings.TrimSpace(message)
		if message == "" {
			continue
		}

		client.Mu.Lock()
		client.MessagesRecv++
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

func (cr *ChatRoom) handleLeave(client *client.Client) {
	cr.mu.Lock()
	if !cr.clients[client] {
		cr.mu.Unlock()
		return
	}
	delete(cr.clients, client)
	cr.mu.Unlock()

	fmt.Printf("%s left (total: %d)\n", client.Name, len(cr.clients))

	// Close channel safely
	select {
	case <-client.Outgoing:
		// Already closed
	default:
		close(client.Outgoing)
	}

	announcement := fmt.Sprintf("*** %s left the chat ***\n", client.Name)
	cr.handleBroadcast(announcement)
}

func (cr *ChatRoom) sendHistory(client *client.Client, count int) {
	cr.messageMu.Lock()
	defer cr.messageMu.Unlock()

	start := len(cr.messages) - count
	if start < 0 {
		start = 0
	}

	historyMsg := "Recent messages:\n"
	for i := start; i < len(cr.messages); i++ {
		msg := cr.messages[i]
		historyMsg += fmt.Sprintf(" [%s]: %s\n", msg.From, msg.Content)
	}

	select {
	case client.Outgoing <- historyMsg:
	default:
	}
}

func (cr *ChatRoom) sendUserList(client *client.Client) {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	list := "Users online:\n"
	for c := range cr.clients {
		status := ""
		if c.IsInactive(1 * time.Minute) {
			status = " (idle)"
		}
		list += fmt.Sprintf("  - %s%s\n", c.Name, status)
	}

	list += fmt.Sprintf("\nTotal messages: %d\n", cr.totalMessages)
	list += fmt.Sprintf("Uptime: %s\n", time.Since(cr.startTime).Round(time.Second))

	select {
	case client.Outgoing <- list:
	default:
	}
}

func (cr *ChatRoom) handleDirectMessage(dm DirectMessage) {
	select {
	case dm.ToClient.Outgoing <- dm.message:
		dm.ToClient.Mu.Lock()
		dm.ToClient.MessagesSent++
		dm.ToClient.Mu.Unlock()
	default:
		fmt.Printf("Couldn't deliver DM to %s\n", dm.ToClient.Name)
	}
}

func (cr *ChatRoom) FindClientByUsername(username string) *client.Client {
	cr.mu.Lock()
	defer cr.mu.Unlock()

	for client := range cr.clients {
		if client.Name == username {
			return client
		}
	}
	return nil
}

func (cr *ChatRoom) handleJoin(client *client.Client) {
	cr.mu.Lock()
	cr.clients[client] = true
	cr.mu.Unlock()

	client.MarkActive()

	fmt.Printf("%s joined (total: %d)\n", client.Name, len(cr.clients))

	cr.sendHistory(client, 10)

	announcement := fmt.Sprintf("*** %s joined the chat ***\n", client.Name)
	cr.handleBroadcast(announcement)
}

func (cr *ChatRoom) handleBroadcast(sms string) {
	// Parse message metadata
	parts := strings.SplitN(sms, ": ", 2)
	from := "system"
	actualContent := sms

	if len(parts) == 2 {
		from = strings.Trim(parts[0], "[]")
		actualContent = parts[1]
	}

	// Create persistent message record
	cr.messageMu.Lock()
	msg := message.Message{
		ID:        cr.nextMessageID,
		From:      from,
		Content:   actualContent,
		Timestamp: time.Now(),
		Channel:   "global",
	}
	cr.nextMessageID++
	cr.messages = append(cr.messages, msg)
	cr.messageMu.Unlock()

	// Persist to WAL
	if err := cr.persistMessage(msg); err != nil {
		fmt.Printf("Failed to persist: %v\n", err)
		// Continue anyway - availability over consistency
	}

	// Collect current clients
	cr.mu.Lock()
	clients := make([]*client.Client, 0, len(cr.clients))
	for client := range cr.clients {
		clients = append(clients, client)
	}
	cr.totalMessages++
	cr.mu.Unlock()

	fmt.Printf("Broadcasting to %d clients: %s", len(clients), sms)

	// Fan-out to all clients
	for _, client := range clients {
		select {
		case client.Outgoing <- sms:
			client.Mu.Lock()
			client.MessagesSent++
			client.Mu.Unlock()
		default:
			fmt.Printf("Skipped %s (channel full)\n", client.Name)
		}
	}
}
