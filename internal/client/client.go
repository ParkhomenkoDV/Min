package client

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

type Client struct {
	Conn       net.Conn    // TCP connection
	Name       string      // Display name
	Outgoing   chan string // Buffered channel for writes
	LastActive time.Time   // For idle detection

	// Statistics
	MessagesSent uint64
	MessagesRecv uint64

	ReconnectToken string
	Mu             sync.Mutex // Protects stats fields
}

func (c *Client) SetActive() {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.LastActive = time.Now()
}

func (c *Client) IsActive(timeout time.Duration) bool {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	return time.Since(c.LastActive) > timeout
}

func Start(address string) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		fmt.Println("Error connecting:", err)
		return
	}
	defer conn.Close()

	fmt.Println("Connected to chat server")

	// Background goroutine: read from server
	go func() {
		reader := bufio.NewReader(conn)
		for {
			message, err := reader.ReadString('\n')
			if err != nil {
				fmt.Println("Disconnected from server.")
				os.Exit(0)
			}
			// Clear current prompt line and print message
			fmt.Print("\r" + message)
			fmt.Print(">> ")
		}
	}()

	// Main goroutine: read from stdin
	inputReader := bufio.NewReader(os.Stdin)
	fmt.Println("Welcome to the chat server!")

	for {
		fmt.Print(">> ")
		message, _ := inputReader.ReadString('\n')
		message = strings.TrimSpace(message)

		if message == "" {
			continue
		}

		conn.Write([]byte(message + "\n"))
	}
}
