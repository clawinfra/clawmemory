# ClawMemory

**Sovereign agent memory engine** — self-hosted, privacy-first memory for OpenClaw agents.

[![CI](https://github.com/clawinfra/clawmemory/actions/workflows/ci.yml/badge.svg)](https://github.com/clawinfra/clawmemory/actions/workflows/ci.yml)

## Overview

ClawMemory is a self-hosted memory system for AI agents, inspired by Supermemory's architecture but with zero third-party data exposure. All data stays in infrastructure you control — local SQLite with optional Turso cloud sync.

**Key capabilities:**
- **Automatic fact extraction** — LLM-based extraction of facts, preferences, and personal details from conversations
- **Hybrid search** — BM25 full-text search + semantic vector search (Reciprocal Rank Fusion)
- **Contradiction resolution** — detects and resolves conflicting facts (newer wins)
- **Temporal decay** — importance decays over time; stale facts are pruned automatically
- **User profile builder** — synthesizes person/preference facts into a structured profile
- **OpenClaw plugin** — TypeScript plugin that auto-injects memory context pre-turn and auto-captures facts post-turn

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                  OpenClaw Plugin (TS)                │
│  Auto-capture (post-turn) + Auto-recall (pre-turn)  │
│  Manual: /remember /recall /profile /forget          │
└────────────────────┬────────────────────────────────┘
                     │ HTTP (localhost:7437)
┌────────────────────▼────────────────────────────────┐
│              ClawMemory Server (Go)                  │
│                                                      │
│  ┌──────────────┐  ┌───────────────┐  ┌──────────┐ │
│  │ Extractor    │  │ Resolver      │  │ Profile  │ │
│  │ (GLM-4.7)   │  │ (contradict.) │  │ Builder  │ │
│  └──────┬───────┘  └───────┬───────┘  └────┬─────┘ │
│         └──────────────────┼───────────────┘        │
│                    ┌───────▼──────┐                  │
│                    │  Store       │                  │
│                    │  (SQLite +   │                  │
│                    │   Turso)     │                  │
│                    └───────┬──────┘                  │
│                            │                         │
│  ┌──────────┐     ┌───────▼──────┐    ┌──────────┐ │
│  │  Search   │     │  Decay       │    │  Embed   │ │
│  │  (BM25+   │     │  (TTL +      │    │  (Ollama │ │
│  │   vector) │     │   importance) │    │  qwen2.5)│ │
│  └──────────┘     └──────────────┘    └──────────┘ │
└─────────────────────────────────────────────────────┘
```

### Storage layers

| Layer | Storage | Use |
|-------|---------|-----|
| Hot | In-context | Injected as `[Memory context]` block pre-turn |
| Warm | Local SQLite | Fast reads, recent facts, BM25 full-text index |
| Cold | Turso cloud | Sync, backup, cross-device |
| Vector | Ollama embeddings | Semantic search (3584-dim cosine similarity) |

## Requirements

- Go 1.22+
- Node.js 22+ (for plugin build)
- Ollama (optional — for vector search, default: `qwen2.5:7b`)
- An OpenAI-compatible LLM endpoint (for fact extraction, e.g. GLM-4.7 or local Ollama)

## Quick Start

```bash
# Clone and build
git clone https://github.com/clawinfra/clawmemory
cd clawmemory
go build ./...

# Start the server (defaults: port 7437, SQLite at ./clawmemory.db)
./clawmemory serve

# Or with custom config
./clawmemory serve --config config.json
```

## Configuration

ClawMemory uses a JSON config file with environment variable overrides.

```json
{
  "server": {
    "port": 7437,
    "auth_token": ""
  },
  "store": {
    "path": "clawmemory.db"
  },
  "embedding": {
    "base_url": "http://localhost:11434",
    "model": "qwen2.5:7b",
    "dimensions": 3584
  },
  "extractor": {
    "base_url": "http://localhost:11434/v1",
    "model": "glm4:latest",
    "max_tokens": 512
  },
  "decay": {
    "half_life_days": 30,
    "min_importance": 0.1,
    "interval_minutes": 60
  }
}
```

Environment overrides: `CLAWMEMORY_PORT`, `CLAWMEMORY_DB_PATH`, `CLAWMEMORY_AUTH_TOKEN`, `CLAWMEMORY_OLLAMA_URL`, `CLAWMEMORY_EXTRACTOR_URL`, `CLAWMEMORY_EXTRACTOR_MODEL`.

## HTTP API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/ingest` | Ingest conversation turns, extract facts |
| `POST` | `/api/v1/recall` | Hybrid BM25 + vector search |
| `POST` | `/api/v1/remember` | Manually store a fact |
| `GET/DELETE` | `/api/v1/forget` | Soft-delete a fact |
| `GET` | `/api/v1/profile` | Get synthesized user profile |
| `GET` | `/api/v1/facts` | List facts with filters |
| `GET` | `/api/v1/stats` | Store statistics |
| `POST` | `/api/v1/sync` | Trigger Turso sync |
| `GET` | `/health` | Health check |

### Example: Ingest

```bash
curl -X POST http://localhost:7437/api/v1/ingest \
  -H "Content-Type: application/json" \
  -d '{"turns": [{"role": "user", "content": "I live in Sydney and work at Anthropic"}]}'
```

### Example: Recall

```bash
curl -X POST http://localhost:7437/api/v1/recall \
  -H "Content-Type: application/json" \
  -d '{"query": "where does the user live?", "limit": 5}'
```

## OpenClaw Plugin

```bash
cd plugin && npm install && npm run build
```

Install the built plugin into OpenClaw to get automatic memory injection and capture on every turn. See `plugin/` for configuration options.

## CLI

```bash
# List facts
./clawmemory facts list --container personal

# Search
./clawmemory recall "user timezone"

# Remember manually
./clawmemory remember "User prefers dark mode"

# Show profile
./clawmemory profile show

# Run benchmark harness (requires running server)
go build -o /tmp/bench ./bench/...
/tmp/bench --server http://localhost:7437 --output /tmp/bench-report.md
```

## Development

```bash
# Run tests
go test ./... -coverprofile=coverage.out

# Check coverage
go tool cover -func=coverage.out | grep total

# Lint
golangci-lint run ./...

# Build plugin
cd plugin && npm run build
```

### Coverage targets

| Package | Target | Current |
|---------|--------|---------|
| config | ≥90% | 95.9% |
| embed | ≥90% | 94.4% |
| extractor | ≥90% | 93.2% |
| decay | ≥90% | 91.9% |
| resolver | ≥90% | 91.8% |
| profile | ≥90% | 90.5% |
| search | ≥90% | 90.3% |
| server | ≥75% | 80.4% |
| store | ≥75% | 76.0% |

## License

MIT — see [LICENSE](LICENSE) for details.
