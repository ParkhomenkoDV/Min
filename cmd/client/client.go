package main

import (
	"Min/internal/client"
	"Min/internal/config"
	"fmt"
)

func main() {
	fmt.Println("Reading config...")
	cfg, err := config.New()
	if err != nil {
		panic(err)
	}

	fmt.Println("Starting client...")
	c := client.New(cfg)
	c.Start()
}
