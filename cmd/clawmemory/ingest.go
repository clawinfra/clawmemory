package main

import (
	"flag"
	"fmt"

	"github.com/clawinfra/clawmemory/internal/ingestor"
)

// runIngest implements the `clawmemory ingest` subcommand.
// It reads a markdown file (local path or remote URL), chunks it by ## headings
// and sends each chunk to the ClawMemory server's /api/v1/ingest endpoint.
func runIngest(args []string) error {
	fs := flag.NewFlagSet("ingest", flag.ExitOnError)

	source := fs.String("source", "", "Local file path or remote URL to ingest (required)")
	agentID := fs.String("agent", "", "Agent ID to use as session_id (required)")
	dryRun := fs.Bool("dry-run", false, "Print chunks without sending to the server")
	serverURL := fs.String("server", "http://localhost:7437", "ClawMemory server base URL")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *source == "" {
		fs.Usage()
		return fmt.Errorf("--source is required")
	}
	if *agentID == "" && !*dryRun {
		fs.Usage()
		return fmt.Errorf("--agent is required (or use --dry-run)")
	}

	src := ingestor.NewMarkdownSource(*source)

	results, err := ingestor.Ingest(src, *agentID, *serverURL, *dryRun)
	if err != nil {
		return fmt.Errorf("ingest: %w", err)
	}

	if len(results) == 0 {
		fmt.Println("No content found — nothing ingested.")
		return nil
	}

	if *dryRun {
		fmt.Printf("Dry-run: %d chunk(s) would be ingested\n\n", len(results))
		for i, r := range results {
			title := r.Chunk.Title
			if title == "" {
				title = "(preamble)"
			}
			preview := r.Chunk.Content
			if len(preview) > 80 {
				preview = preview[:80] + "…"
			}
			fmt.Printf("  [%d] %s\n      %s\n", i+1, title, preview)
		}
		return nil
	}

	ok, failed := 0, 0
	for _, r := range results {
		if r.Err != nil {
			failed++
			title := r.Chunk.Title
			if title == "" {
				title = "(preamble)"
			}
			fmt.Printf("  FAIL [%s]: %v\n", title, r.Err)
		} else {
			ok++
		}
	}

	fmt.Printf("Ingested %d/%d chunk(s) successfully.\n", ok, len(results))
	if failed > 0 {
		return fmt.Errorf("%d chunk(s) failed to ingest", failed)
	}
	return nil
}
