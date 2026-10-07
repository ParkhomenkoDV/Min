package client

import (
	"Min/internal/config"
	"Min/internal/statistic"

	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

type Client struct {
	Config *config.Config

	Conn       net.Conn    // TCP connection
	Name       string      // Display name
	Outgoing   chan string // Buffered channel for writes
	LastActive time.Time   // For idle detection

	Statistic statistic.Statistic

	Token string
	Mu    sync.Mutex // Protects stats fields
}

func New(
	cfg *config.Config,
	conn net.Conn,
	name, tok string,
) *Client {
	return &Client{
		Config: cfg,

		Conn:  conn,
		Name:  name,
		Token: tok,

		Outgoing:   make(chan string, 10), // Buffered
		LastActive: time.Now(),
	}
}

func (c *Client) SetActive() {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	c.LastActive = time.Now()
}

func (c *Client) IsActive(timeout time.Duration) bool {
	c.Mu.Lock()
	defer c.Mu.Unlock()
	return time.Since(c.LastActive) < timeout
}

func (c *Client) Start() {
	conn, err := net.Dial(c.Config.Network, c.Config.Address)
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
		sms, _ := inputReader.ReadString('\n')
		sms = strings.TrimSpace(sms)
		if sms == "" {
			continue
		}

		conn.Write([]byte(sms + "\n"))
	}
}

func (c *Client) WriteMessages() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("Panic in writeMessages for %s: %v\n", c.Name, r)
		}
	}()

	writer := bufio.NewWriter(c.Conn)

	for message := range c.Outgoing {
		_, err := writer.WriteString(message)
		if err != nil {
			fmt.Printf("Write error for %s: %v\n", c.Name, err)
			return
		}

		err = writer.Flush()
		if err != nil {
			fmt.Printf("Flush error for %s: %v\n", c.Name, err)
			return
		}
	}
}
