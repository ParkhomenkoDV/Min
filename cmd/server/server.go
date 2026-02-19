package main

import (
	"Min/internal/chatroom"
	"fmt"
	"os"
)

func main() {
	fmt.Println("Starting server from cmd/server...")
	chatroom.StartServer()
	os.Exit(0)
}
