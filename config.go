package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	DomainName  string `json:"domain_name"`
	AuthEnabled bool   `json:"auth_enabled"`
	Username    string `json:"username"`
	SSLEnabled  bool   `json:"ssl_enabled"`
}

// LoadConfig reads config from a JSON file, applies defaults if fields are empty.
func LoadConfig(path string) (Config, error) {
	// Default config
	cfg := Config{
		DomainName:  "http://localhost",
		AuthEnabled: false,
		Username:    "user",
		SSLEnabled:  false,
	}

	file, err := os.Open(path)
	if err != nil {
		return cfg, fmt.Errorf("could not open config file: %v", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("could not decode config JSON: %v", err)
	}

	return cfg, nil
}
