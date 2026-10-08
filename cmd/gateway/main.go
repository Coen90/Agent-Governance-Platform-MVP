package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"agent-gateway-mvp/internal/config"
	"agent-gateway-mvp/internal/httpapi"
	"agent-gateway-mvp/internal/postgres"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	db, err := postgres.Open(ctx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return fmt.Errorf("database initialization failed: %w", err)
	}
	defer db.Close()

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           httpapi.NewHandler(postgres.New(db), cfg.AgentToken, cfg.AdminToken),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("agent gateway listening on %s", cfg.ListenAddr)
	return server.ListenAndServe()
}
