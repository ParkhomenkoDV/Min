package main

import (
	"Min/internal/chatroom"
	"fmt"
)

func main() {
	fmt.Println("Starting client from cmd/client...")
	chatroom.StartClient()
}
