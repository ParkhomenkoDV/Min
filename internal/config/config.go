package config

import (
	"flag"
	"fmt"
	"strings"
)

type Config struct {
	Network string
	Address string
}

func New() (*Config, error) {
	flags := parse()

	err := validate(flags)
	if err != nil {
		return &Config{}, err
	}

	return &Config{
		Network: flags.Network,
		Address: flags.Address,
	}, nil
}

func parse() *Config {
	network := flag.String(
		"network",
		"tcp",
		"",
	)
	address := flag.String(
		"address",
		":9000",
		"",
	)

	flag.Parse()

	return &Config{
		Network: strings.ToLower(*network),
		Address: *address,
	}
}

func validate(flags *Config) error {
	if flags.Network != "tcp" {
		return fmt.Errorf("invalid network: '%s'", flags.Network)
	}

	if flags.Address == "" || !strings.Contains(flags.Address, ":") {
		return fmt.Errorf("invalid address: '%s'", flags.Address)
	}

	return nil
}
