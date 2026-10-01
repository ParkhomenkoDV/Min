package main

import (
	"Min/internal/server"
	"fmt"
	"os"
)

func main() {
	fmt.Println("Starting server from cmd/server...")
	server.Start(":9000")
	os.Exit(0)
}
