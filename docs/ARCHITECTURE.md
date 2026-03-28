# Architecture — clawmemory

## Overview

ClawMemory is a self-hosted sovereign memory engine for OpenClaw agents. It extracts facts from conversations using GLM-4.7, stores them in SQLite (with optional Turso cloud sync), and retrieves them via hybrid BM25+vector search (Reciprocal Rank Fusion). The OpenClaw TypeScript plugin auto-injects memory context pre-turn and auto-captures new facts post-turn — zero manual wiring needed.

## Directory Structure

```
clawmemory/
├── cmd/                  # CLI entry point (serve, migrate, search, forget, profile)
├── internal/
│   ├── config/           # JSON/YAML config + env var overrides
│   ├── decay/            # Exponential half-life importance decay + TTL pruning
│   ├── embed/            # Ollama embedding HTTP client (graceful degradation)
│   ├── extractor/        # GLM-4.7 LLM fact extraction (OpenAI-compatible API)
│   ├── profile/          # User profile builder (synthesizes person+preference facts)
│   ├── resolver/         # Contradiction detection and resolution (newer wins)
│   ├── search/           # BM25 (SQLite FTS5) + vector search + RRF fusion
│   ├── server/           # HTTP API server (localhost:7437)
│   └── store/            # SQLite store + Turso sync, WAL mode, FTS5, migrations
├── plugin/               # OpenClaw TypeScript plugin (auto-capture + auto-recall)
├── bench/                # Benchmark scripts
├── docs/                 # Architecture, conventions, quality docs
└── scripts/              # Setup and utility scripts
```

## Layer Rules

```
cmd/          →  internal/*     ← allowed (cmd wires everything)
internal/*    →  internal/store ← allowed (all packages use store)
internal/*    →  internal/config ← allowed (all packages read config)
extractor     →  (no internal deps) ← allowed (pure HTTP client)
profile       →  extractor, store ← allowed
resolver      →  store ← allowed
search        →  store, embed ← allowed
decay         →  store ← allowed
server        →  all internal/* ← allowed (server orchestrates)
internal/*    →  cmd/  ← FORBIDDEN
store         →  embed/extractor/search ← FORBIDDEN (store is leaf)
```

## Key Packages

| Package | Responsibility |
|---------|---------------|
| `config` | JSON/YAML config loading with env var overrides. All tunables (LLM endpoint, Ollama URL, SQLite path, decay params) live here. |
| `store` | SQLite + libsql driver. WAL mode, FTS5 virtual table for BM25. Migrations auto-run on open. Optional Turso sync via libsql. |
| `extractor` | Sends conversation turns to GLM-4.7 (OpenAI-compatible) and parses structured fact JSON. Handles retries and bad JSON gracefully. |
| `embed` | Ollama embedding client (default: `qwen2.5:7b`, 3584-dim). Gracefully degrades to BM25-only if Ollama is unreachable. |
| `search` | BM25 (SQLite FTS5) + cosine vector search fused via RRF (k=60). Returns `[]Result{FactID, Score, Fact}`. |
| `resolver` | Detects contradictions (same topic, different value) among new+existing facts. Strategy: newer fact wins, older demoted. |
| `decay` | Exponential half-life decay on importance score. Default: half-life=30 days, min=0.1. Prunes facts below threshold. Runs on interval. |
| `profile` | Synthesizes `person` and `preference` category facts into a structured `Profile` + LLM-generated natural language summary. |
| `server` | HTTP API on `:7437`. Routes: POST /facts, GET /search, GET /profile, DELETE /facts/:id, POST /forget. CORS + logging middleware. |

## Dependency Injection

All packages use constructor injection:
```go
// Example: search package wires BM25 + vector
s := search.NewHybrid(store, embedClient)

// Example: server wires all internals
srv := server.New(cfg, store, extractor, resolver, decay, searchSvc, profileBuilder)
```
No global state. All dependencies passed explicitly. Tests swap in mock stores/HTTP servers.

## Key Invariants

1. **Store is the leaf** — `store` imports nothing from other internal packages
2. **Embed degrades gracefully** — if Ollama is down, search falls back to BM25-only (no hard failure)
3. **Extractor is stateless** — each call is a pure HTTP POST; no internal state
4. **Contradiction resolution is non-destructive** — demoted facts are soft-deleted (importance set to 0), not purged; allows rollback
5. **FTS5 is the source of truth for BM25** — never re-implement scoring; use SQLite's native `rank` column
6. **Migrations are append-only** — never modify existing migration SQL; add new migrations only
7. **All handlers return JSON** — even errors: `{"error": "message"}` with appropriate status code
