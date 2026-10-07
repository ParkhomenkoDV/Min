package main

import (
	"Min/internal/config"
	"Min/internal/server"

	"fmt"
	"os"
)

func main() {
	fmt.Println("Reading config...")
	cfg, err := config.New()
	if err != nil {
		panic(err)
	}

	fmt.Println("Starting server...")
	s := server.New(cfg)
	s.Start()

	os.Exit(0) // TODO why
}
