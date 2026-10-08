package config

import (
	"errors"
	"os"
)

type Config struct {
	DatabaseURL string
	ListenAddr  string
	AgentToken  string
	AdminToken  string
}

func Load() (Config, error) {
	cfg := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		ListenAddr:  os.Getenv("LISTEN_ADDR"),
		AgentToken:  os.Getenv("AGENT_TOKEN"),
		AdminToken:  os.Getenv("ADMIN_TOKEN"),
	}
	if len(cfg.AgentToken) < 16 || len(cfg.AdminToken) < 16 || cfg.AgentToken == cfg.AdminToken {
		return Config{}, errors.New("set different AGENT_TOKEN and ADMIN_TOKEN values (at least 16 characters)")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "127.0.0.1:8080"
	}
	return cfg, nil
}
