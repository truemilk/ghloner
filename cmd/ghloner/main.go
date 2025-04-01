package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/truemilk/ghloner/internal/config"
	"github.com/truemilk/ghloner/internal/logger"
	"github.com/truemilk/ghloner/internal/repository"
)

func main() {
	// Initialize logger with default settings
	if err := logger.Init("info", "text"); err != nil {
		slog.Error("Failed to initialize logger", "error", err)
		os.Exit(1)
	}

	cfg, err := config.Parse()
	if err != nil {
		slog.Error("Configuration error", "error", err)
		os.Exit(1)
	}

	client, err := config.NewGitHubClient(cfg.Token)
	if err != nil {
		slog.Error("Failed to create GitHub client", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-signalChan
		slog.Warn("Received interrupt signal. Gracefully shutting down... (Press Ctrl+C again to force quit)")
		cancel()

		<-signalChan
		slog.Info("Force quitting...")
		os.Exit(1)
	}()

	processor := repository.NewProcessor(client, cfg)
	if err := processor.Run(ctx); err != nil {
		slog.Error("Error during processing", "error", err)
		os.Exit(1)
	}
}
