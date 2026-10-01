package main

import (
	"Min/internal/client"
	"fmt"
)

func main() {
	fmt.Println("Starting client from cmd/client...")
	client.Start(":9000")
}
