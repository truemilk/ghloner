package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/truemilk/ghloner/internal/config"
	"github.com/truemilk/ghloner/internal/repository"
)

func main() {
	cfg, err := config.Parse()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Create GitHub client
	client, err := config.NewGitHubClient(cfg.Token)
	if err != nil {
		fmt.Printf("Error creating GitHub client: %v\n", err)
		os.Exit(1)
	}

	// Set up signal handling
	ctx, cancel := context.WithCancel(context.Background())
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	// Handle shutdown in a separate goroutine
	go func() {
		<-signalChan
		fmt.Println("\nReceived interrupt signal. Gracefully shutting down...")
		fmt.Println("(Press Ctrl+C again to force quit)")
		cancel()

		<-signalChan
		fmt.Println("\nForce quitting...")
		os.Exit(1)
	}()

	// Create and run processor
	processor := repository.NewProcessor(client, cfg)
	if err := processor.Run(ctx); err != nil {
		fmt.Printf("Error during processing: %v\n", err)
		os.Exit(1)
	}
}
