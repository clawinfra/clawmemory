# ClawMemory

**Sovereign agent memory engine** — self-hosted, privacy-first memory for OpenClaw agents. No vector DB, no embeddings, no external dependencies. Just SQLite + BM25 + LLM.

[![CI](https://github.com/clawinfra/clawmemory/actions/workflows/ci.yml/badge.svg)](https://github.com/clawinfra/clawmemory/actions/workflows/ci.yml)

## Overview

ClawMemory is a self-hosted memory system for AI agents. All data stays in infrastructure you control — local SQLite with optional Turso cloud sync.

Inspired by [Karpathy's LLM Knowledge Base](https://github.com/AlexChen31337/llm-knowledge-base) methodology: no RAG, no vector DB. The LLM reads, extracts, and organises — search is pure BM25 full-text via SQLite FTS5.

**Key capabilities:**
- **Automatic fact extraction** — LLM-based extraction of facts, preferences, and personal details from conversations
- **BM25 full-text search** — fast keyword search via SQLite FTS5 (no embeddings, no vector DB)
- **Contradiction resolution** — detects and resolves conflicting facts (newer wins)
- **Temporal decay** — importance decays over time; stale facts are pruned automatically
- **User profile builder** — synthesises person/preference facts into a structured profile
- **OpenClaw plugin** — TypeScript plugin that auto-injects memory context pre-turn and auto-captures facts post-turn

### Why no vector search?

At agent-memory scale (~10K facts), BM25 full-text search is faster, simpler, and more predictable than semantic vector search:

| | BM25 (FTS5) | Vector Search |
|---|---|---|
| **Latency** | ~1ms | ~3000ms (embed + cosine) |
| **Dependencies** | SQLite only | Ollama + GPU + embedding model |
| **Accuracy** | Excellent for structured facts | Better for fuzzy semantic queries |
| **Complexity** | Zero infra | Embedding server + shim + model |
| **CPU cost** | Negligible | Significant (embedding computation) |

For structured, LLM-extracted facts with clear keywords, BM25 wins. The LLM already did the semantic work during extraction.

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
│  │ (LLM-based)  │  │ (contradict.) │  │ Builder  │ │
│  └──────┬───────┘  └───────┬───────┘  └────┬─────┘ │
│         └──────────────────┼───────────────┘        │
│                    ┌───────▼──────┐                  │
│                    │  Store       │                  │
│                    │  (SQLite +   │                  │
│                    │   Turso)     │                  │
│                    └───────┬──────┘                  │
│                            │                         │
│  ┌──────────┐     ┌───────▼──────┐                  │
│  │  Search   │     │  Decay       │                  │
│  │  (BM25    │     │  (TTL +      │                  │
│  │   FTS5)   │     │   importance) │                  │
│  └──────────┘     └──────────────┘                  │
└─────────────────────────────────────────────────────┘
```

### Storage layers

| Layer | Storage | Use |
|-------|---------|-----|
| Hot | In-context | Injected as `[Memory context]` block pre-turn |
| Warm | Local SQLite | Fast reads, recent facts, BM25 full-text index |
| Cold | Turso cloud | Sync, backup, cross-device |

## Requirements

- Go 1.22+
- Node.js 22+ (for plugin build)
- An OpenAI-compatible LLM endpoint (for fact extraction, e.g. GLM-4.7 or local Ollama)

## Quick Start

```bash
# Clone and build
git clone https://github.com/clawinfra/clawmemory
cd clawmemory
go build -o clawmemory ./cmd/...

# Start the server (defaults: port 7437, SQLite at ./clawmemory.db)
./clawmemory

# Or with custom config
CLAWMEMORY_PORT=7437 ./clawmemory
```

## Configuration

ClawMemory uses a JSON config file with environment variable overrides.

```json
{
  "server": {
    "host": "127.0.0.1",
    "port": 7437,
    "auth_token": ""
  },
  "store": {
    "path": "clawmemory.db"
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

Environment overrides: `CLAWMEMORY_PORT`, `CLAWMEMORY_DB_PATH`, `CLAWMEMORY_AUTH_TOKEN`, `CLAWMEMORY_EXTRACTOR_URL`, `CLAWMEMORY_EXTRACTOR_MODEL`.

## HTTP API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/ingest` | Ingest conversation turns, extract facts |
| `POST` | `/api/v1/recall` | BM25 full-text search over facts |
| `POST` | `/api/v1/remember` | Manually store a fact |
| `GET/DELETE` | `/api/v1/forget` | Soft-delete a fact |
| `GET` | `/api/v1/profile` | Get synthesised user profile |
| `GET` | `/api/v1/facts` | List facts with filters |
| `GET` | `/api/v1/stats` | Store statistics |
| `POST` | `/api/v1/sync` | Trigger Turso sync |
| `GET` | `/health` | Health check |

### Example: Ingest

```bash
curl -X POST http://localhost:7437/api/v1/ingest \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "chat-001",
    "turns": [
      {"role": "user", "content": "I live in Sydney and work on ClawChain"},
      {"role": "assistant", "content": "Got it! Sydney-based, working on ClawChain."}
    ]
  }'
```

### Example: Recall

```bash
curl -X POST http://localhost:7437/api/v1/recall \
  -H "Content-Type: application/json" \
  -d '{"query": "where does the user live?", "limit": 5}'
```

### Example: Remember

```bash
curl -X POST http://localhost:7437/api/v1/remember \
  -H "Content-Type: application/json" \
  -d '{"content": "User prefers dark mode", "category": "preference", "importance": 0.8}'
```

Valid categories: `person`, `project`, `preference`, `event`, `technical`, `general`
Valid containers: `work`, `trading`, `clawchain`, `personal`, `general`

## OpenClaw Plugin

The TypeScript plugin auto-injects memory context before each turn and captures new facts after each turn.

```bash
cd plugin && npm install && npm run build
```

Install into `~/.openclaw/extensions/clawmemory/` for automatic memory integration. See `plugin/` for configuration options.

## Integration with LLM Knowledge Base

ClawMemory works alongside the [LLM Knowledge Base](https://github.com/AlexChen31337/llm-knowledge-base) skill:

- **ClawMemory** handles real-time, structured fact storage (conversations → extracted facts → BM25 search)
- **KB Skill** handles long-form knowledge compilation (raw materials → wiki articles → full-text grep search)

Both follow the same philosophy: no vector DB, no embeddings. Let the LLM do the semantic heavy lifting.

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

| Package | Target |
|---------|--------|
| config | ≥90% |
| extractor | ≥90% |
| decay | ≥90% |
| resolver | ≥90% |
| profile | ≥90% |
| search | ≥90% |
| server | ≥75% |
| store | ≥75% |

## Changelog

### v0.2.0 (2026-04-06)
- **Removed** vector search, embedding client, and Ollama dependency
- **Removed** `internal/embed/` package, `search/vector.go`, `ollama-embed-shim.py`
- **Simplified** search to BM25-only via SQLite FTS5
- **Simplified** resolver to use BM25 for contradiction detection
- **Performance** recall latency: 3000ms → 1ms (no embedding overhead)
- **Zero GPU dependency** — runs on any machine with Go + SQLite

### v0.1.0 (2026-03-24)
- Initial release
- Hybrid BM25 + vector search (Reciprocal Rank Fusion)
- LLM-based fact extraction, contradiction resolution, profile building
- Temporal decay, Turso sync, OpenClaw plugin

## License

MIT — see [LICENSE](LICENSE) for details.
