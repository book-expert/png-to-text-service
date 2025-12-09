package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/book-expert/logger"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/book-expert/png-to-text-service/internal/config"
	"github.com/book-expert/png-to-text-service/internal/llm"
	"github.com/book-expert/png-to-text-service/internal/worker"
)

const (
	ShutdownGracePeriodSeconds = 2
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Fatal error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// 1. Load Config
	// We use a temporary logger for config loading errors
	tempLog, _ := logger.New("", "")
	cfg, err := config.Load("project.toml", tempLog)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// 2. Setup Logger
	log, err := logger.New(cfg.Service.LogDir, "png-to-text.log")
	if err != nil {
		return fmt.Errorf("setup logger: %w", err)
	}
	defer func() {
		if err := log.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to close logger: %v\n", err)
		}
	}()

	log.Info("Starting PNG-to-Text Service...")

	// 3. Initialize LLM Processor
	apiKey := cfg.GetAPIKey()
	if apiKey == "" {
		return errors.New("LLM API key not found in environment")
	}

	llmCfg := &llm.Config{
		APIKey:            apiKey,
		BaseURL:           cfg.LLM.BaseURL,
		Model:             cfg.LLM.Model,
		Temperature:       cfg.LLM.Temperature,
		TimeoutSeconds:    cfg.LLM.TimeoutSeconds,
		MaxRetries:        cfg.LLM.MaxRetries,
		SystemInstruction: cfg.LLM.Prompts.SystemInstruction,
		ExtractionPrompt:  cfg.LLM.Prompts.ExtractionPrompt,
	}
	llmProc := llm.NewProcessor(llmCfg, log)

	// 4. Setup NATS
	nc, js, err := setupNATS(cfg)
	if err != nil {
		return fmt.Errorf("setup nats: %w", err)
	}
	defer nc.Close()

	// 5. Initialize and Run Worker
	// We need to resolve the object stores first
	pngStore, err := js.ObjectStore(context.Background(), cfg.NATS.ObjectStore.PNGBucket)
	if err != nil {
		return fmt.Errorf("bind png store: %w", err)
	}
	textStore, err := js.ObjectStore(context.Background(), cfg.NATS.ObjectStore.TextBucket)
	if err != nil {
		return fmt.Errorf("bind text store: %w", err)
	}

	w := worker.New(
		js,
		cfg.NATS.Consumer.Stream,
		cfg.NATS.Consumer.Subject,
		cfg.NATS.Consumer.Durable,
		cfg.NATS.Producer.Subject,
		cfg.NATS.DLQSubject,
		llmProc,
		log,
		pngStore,
		textStore,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	log.Info("Worker running. Listening on %s", cfg.NATS.Consumer.Subject)
	if err := w.Start(ctx); err != nil {
		log.Error("Worker stopped: %v", err)
		return err
	}

	log.Info("Shutdown complete.")
	return nil
}

func setupNATS(cfg *config.Config) (*nats.Conn, jetstream.JetStream, error) {
	nc, err := nats.Connect(cfg.NATS.URL)
	if err != nil {
		return nil, nil, err
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, nil, err
	}

	return nc, js, nil
}
