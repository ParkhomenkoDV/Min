package server

import (
	"Min/internal/chatroom"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
)

type Server struct {
}

func Start(address string) {
	chatRoom, err := chatroom.New("./data")
	if err != nil {
		fmt.Printf("Failed to initialize: %v\n", err)
		return
	}
	defer chatRoom.Shutdown()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nReceived shutdown signal")
		chatRoom.Shutdown()
		os.Exit(0)
	}()

	go chatRoom.Run()

	listener, err := net.Listen("tcp", address)
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
		go chatroom.HandleClient(conn, chatRoom)
	}
}
