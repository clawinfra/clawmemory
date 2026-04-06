// ClawMemory — Sovereign Agent Memory Engine
// Provides persistent, privacy-first memory for OpenClaw agents.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/clawinfra/clawmemory/internal/config"
	"github.com/clawinfra/clawmemory/internal/server"
)

// version is set at build time via -ldflags.
var version = "0.2.0"

func main() {
	if err := run(); err != nil {
		log.Fatalf("clawmemory: %v", err)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return runServe([]string{})
	}

	subcommand := os.Args[1]
	args := os.Args[2:]

	switch subcommand {
	case "serve":
		return runServe(args)
	case "stats":
		return runStats(args)
	case "version":
		fmt.Printf("clawmemory %s\n", version)
		return nil
	case "help", "--help", "-h":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown subcommand: %s", subcommand)
	}
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "Path to config JSON file")
	host := fs.String("host", "", "Override server host")
	port := fs.Int("port", 0, "Override server port")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if *host != "" {
		cfg.Server.Host = *host
	}
	if *port != 0 {
		cfg.Server.Port = *port
	}

	srv, err := server.New(cfg)
	if err != nil {
		return fmt.Errorf("create server: %w", err)
	}

	log.Printf("[clawmemory] Starting server on %s:%d", cfg.Server.Host, cfg.Server.Port)

	// Handle graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-quit:
		log.Println("[clawmemory] Shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	case err := <-errCh:
		return err
	}
}

func runStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	serverURL := fs.String("server", "http://127.0.0.1:7437", "ClawMemory server URL")
	if err := fs.Parse(args); err != nil {
		return err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(*serverURL + "/api/v1/stats")
	if err != nil {
		return fmt.Errorf("get stats: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read stats response: %w", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		fmt.Println(string(body))
		return nil
	}

	b, _ := json.MarshalIndent(data, "", "  ")
	fmt.Println(string(b))
	return nil
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `ClawMemory — Sovereign Agent Memory Engine v%s

Usage:
  clawmemory [subcommand] [flags]

Subcommands:
  serve   Start the HTTP server (default)
  stats   Print store statistics from a running server
  version Print version

Examples:
  clawmemory serve
  clawmemory serve --config ~/.clawmemory/config.json
  clawmemory serve --port 7438
  clawmemory stats --server http://127.0.0.1:7437

`, version)
}
