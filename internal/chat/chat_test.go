package chat

import (
	"Min/internal/client"
	"strings"
	"testing"
	"time"
)

func TestBroadcast(t *testing.T) {
	cr, _ := New("./testdata")
	defer cr.Shutdown()

	go cr.Start()

	// Create mock clients
	client1 := &client.Client{
		Name:     "Alice",
		Outgoing: make(chan string, 10),
	}
	client2 := &client.Client{
		Name:     "Bob",
		Outgoing: make(chan string, 10),
	}

	// Join clients
	cr.Join <- client1
	cr.Join <- client2
	time.Sleep(100 * time.Millisecond)

	// Broadcast message
	cr.broadcast <- "[Alice]: Hello!"

	// Verify both receive it
	select {
	case msg := <-client1.Outgoing:
		if !strings.Contains(msg, "Hello!") {
			t.Fatal("Client1 didn't receive correct message")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Client1 didn't receive message")
	}

	select {
	case msg := <-client2.Outgoing:
		if !strings.Contains(msg, "Hello!") {
			t.Fatal("Client2 didn't receive correct message")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Client2 didn't receive message")
	}
}
