package server

import (
	"Min/internal/chat"
	"Min/internal/config"

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

	go ch.Run()

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
		go chat.Run(conn, ch)
	}
}
