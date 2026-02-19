package chatroom

import (
	"strings"
	"testing"
	"time"
)

func TestBroadcast(t *testing.T) {
	cr, _ := NewChatRoom("./testdata")
	defer cr.shutdown()

	go cr.Run()

	// Create mock clients
	client1 := &Client{
		username: "Alice",
		outgoing: make(chan string, 10),
	}
	client2 := &Client{
		username: "Bob",
		outgoing: make(chan string, 10),
	}

	// Join clients
	cr.join <- client1
	cr.join <- client2
	time.Sleep(100 * time.Millisecond)

	// Broadcast message
	cr.broadcast <- "[Alice]: Hello!"

	// Verify both receive it
	select {
	case msg := <-client1.outgoing:
		if !strings.Contains(msg, "Hello!") {
			t.Fatal("Client1 didn't receive correct message")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Client1 didn't receive message")
	}

	select {
	case msg := <-client2.outgoing:
		if !strings.Contains(msg, "Hello!") {
			t.Fatal("Client2 didn't receive correct message")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Client2 didn't receive message")
	}
}
