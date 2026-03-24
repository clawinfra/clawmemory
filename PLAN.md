# ClawMemory — Sovereign Agent Memory System
## Architecture Plan v1.0 — 2026-03-24

---

## Vision

A **self-hosted, privacy-first memory engine** for OpenClaw agents — inspired by Supermemory's architecture but with zero third-party data exposure. All data stays in infrastructure we control (local + Turso cloud).

**The thesis:** Supermemory's benchmarks prove what good memory looks like. We can build the same capabilities — fact extraction, contradiction resolution, temporal forgetting, hybrid RAG — but own every byte.

---

## What We're Building

**`clawmemory`** — a Go library + OpenClaw plugin + CLI

```
github.com/clawinfra/clawmemory
```

### Core capabilities (matching Supermemory's benchmark criteria)

| Capability | How |
|-----------|-----|
| Fact extraction from conversations | LLM call (GLM-4.7, cheap) post-turn |
| Contradiction resolution | Version facts with timestamps, newer wins |
| Temporal forgetting | TTL + importance decay, auto-prune |
| Hybrid search (semantic + keyword) | Ollama embeddings (qwen2.5:7b) + BM25 |
| User profile | Persistent stable facts + recent context |
| Multi-container (namespacing) | Projects: `work`, `trading`, `clawchain`, etc. |

---

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                  OpenClaw Plugin                     │
│  Auto-capture (post-turn) + Auto-recall (pre-turn)  │
└────────────────────┬────────────────────────────────┘
                     │ HTTP / gRPC
┌────────────────────▼────────────────────────────────┐
│              ClawMemory Server (Go)                  │
│                                                      │
│  ┌──────────────┐  ┌───────────────┐  ┌──────────┐ │
│  │ Fact         │  │ Contradiction │  │ Profile  │ │
│  │ Extractor    │  │ Resolver      │  │ Builder  │ │
│  └──────┬───────┘  └───────┬───────┘  └────┬─────┘ │
│         └──────────────────┼───────────────┘        │
│                    ┌───────▼──────┐                  │
│                    │  Memory      │                  │
│                    │  Store       │                  │
│                    └───────┬──────┘                  │
│                            │                         │
│         ┌──────────────────┼──────────────────┐      │
│         ▼                  ▼                  ▼      │
│   ┌──────────┐    ┌──────────────┐    ┌──────────┐  │
│   │  SQLite  │    │   Turso      │    │  Vector  │  │
│   │  (local) │◄──►│  (cloud sync)│    │  Index   │  │
│   └──────────┘    └──────────────┘    │ (Ollama) │  │
│                                       └──────────┘  │
└─────────────────────────────────────────────────────┘
```

### Storage layers
- **Hot:** In-context (injected by OpenClaw plugin pre-turn)
- **Warm:** Local SQLite — fast reads, recent facts, full-text index
- **Cold:** Turso cloud — sync, backup, cross-device
- **Vector:** Ollama `qwen2.5:7b` embeddings on GPU server (peter@10.0.0.44) — semantic search

---

## Repo Structure

```
clawmemory/
├── cmd/
│   └── clawmemory/          # CLI binary
│       └── main.go
├── internal/
│   ├── extractor/           # LLM-based fact extraction
│   │   ├── extractor.go
│   │   └── extractor_test.go
│   ├── store/               # Storage layer
│   │   ├── sqlite.go
│   │   ├── turso.go
│   │   ├── store.go
│   │   └── store_test.go
│   ├── search/              # Hybrid search (BM25 + vector)
│   │   ├── bm25.go
│   │   ├── vector.go        # Ollama embedding client
│   │   ├── hybrid.go
│   │   └── search_test.go
│   ├── resolver/            # Contradiction detection & resolution
│   │   ├── resolver.go
│   │   └── resolver_test.go
│   ├── profile/             # User profile builder
│   │   ├── profile.go
│   │   └── profile_test.go
│   ├── decay/               # Temporal forgetting / importance decay
│   │   ├── decay.go
│   │   └── decay_test.go
│   └── server/              # HTTP server for OpenClaw plugin
│       ├── server.go
│       └── server_test.go
├── plugin/                  # OpenClaw plugin (TypeScript)
│   ├── package.json
│   ├── src/
│   │   ├── index.ts         # Plugin entry point
│   │   ├── capture.ts       # Auto-capture post-turn
│   │   ├── recall.ts        # Auto-recall pre-turn
│   │   └── tools.ts         # /remember, /recall commands
│   └── tsconfig.json
├── bench/                   # Benchmark suite
│   ├── longmemeval/         # LongMemEval-inspired tests
│   ├── locomo/              # LoCoMo-inspired tests
│   └── runner.go
├── docs/
│   ├── ARCHITECTURE.md
│   ├── BENCHMARK.md
│   └── API.md
├── scripts/
│   ├── setup.sh             # One-command install
│   └── bench.sh             # Run full benchmark suite
├── AGENTS.md
├── go.mod
└── README.md
```

---

## Data Model

```sql
-- Core facts table
CREATE TABLE facts (
    id          TEXT PRIMARY KEY,      -- UUID
    content     TEXT NOT NULL,         -- "Bowen's timezone is Australia/Sydney"
    category    TEXT NOT NULL,         -- person|project|preference|event|technical
    container   TEXT NOT NULL,         -- work|trading|clawchain|personal
    importance  REAL DEFAULT 0.7,      -- 0.0-1.0
    confidence  REAL DEFAULT 1.0,      -- drops on contradiction
    source      TEXT,                  -- conversation turn ID
    created_at  INTEGER NOT NULL,      -- unix timestamp
    updated_at  INTEGER NOT NULL,
    expires_at  INTEGER,               -- NULL = never expires
    superseded_by TEXT,                -- FK to newer fact (contradiction chain)
    embedding   BLOB                   -- float32[] from Ollama
);

-- Conversation turns (for extraction context)
CREATE TABLE turns (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL,
    role        TEXT NOT NULL,         -- user|assistant
    content     TEXT NOT NULL,
    created_at  INTEGER NOT NULL,
    processed   INTEGER DEFAULT 0      -- 0=pending extraction, 1=done
);

-- User profile (stable facts + recent summary)
CREATE TABLE profile (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  INTEGER NOT NULL
);
```

---

## Benchmark Suite

Inspired by the 3 benchmarks Supermemory tops:

### 1. LongMemEval (ICLR 2025 — 500 questions, 5 abilities)
- **Information Extraction** — "What is Bowen's preferred timezone?"
- **Multi-Session Reasoning** — "What project was Alex working on when BTC hit $70K?"
- **Knowledge Updates** — "What's the current ClawChain block height?" (facts change over time)
- **Temporal Reasoning** — "What did we discuss 3 sessions ago about the tax audit?"
- **Abstention** — "What's Bowen's mother's name?" (should say: I don't know)

### 2. LoCoMo-style (Long Conversation Memory)
- Feed 50-turn conversation history
- Ask questions requiring synthesis across multiple turns
- Measure recall@1, recall@5, MRR

### 3. ConvoMem-style (Contradiction handling)
- Feed conflicting facts at different time points
- Verify system uses the newer fact
- Verify old fact is marked superseded (not deleted — auditable)

### Metrics we'll report
| Metric | Description |
|--------|-------------|
| Recall@1 | Top result is correct |
| Recall@5 | Correct in top 5 |
| MRR | Mean Reciprocal Rank |
| Contradiction Accuracy | % correct resolution |
| Abstention F1 | Don't hallucinate unknown facts |
| Latency p50/p99 | Search + injection speed |
| Extraction Quality | Human-eval on fact quality |

---

## OpenClaw Plugin Behaviour

**Auto-capture (post-turn):**
1. After every conversation turn, send last 2 turns to ClawMemory server
2. LLM extracts 0-5 facts (GLM-4.7, ~$0.001/call)
3. Contradiction check against existing facts
4. Store with embedding

**Auto-recall (pre-turn):**
1. Before every turn, query ClawMemory with current message
2. BM25 keyword search + semantic search → re-rank → top 10
3. Inject as `[Memory context]` block into system prompt
4. Inject user profile summary every 50 turns

**Commands:**
- `/remember <text>` — manual store
- `/recall <query>` — manual search with scores
- `/profile` — show current user profile
- `/forget <query>` — mark facts as deleted

---

## Build Phases

### Phase 1 — Core engine (Week 1)
- [ ] SQLite store + Turso sync
- [ ] Ollama embedding client
- [ ] BM25 + hybrid search
- [ ] HTTP server
- [ ] Basic fact extraction (LLM call)
- [ ] ≥90% test coverage on store + search

### Phase 2 — Intelligence (Week 2)
- [ ] Contradiction resolver
- [ ] Importance decay (time-weighted)
- [ ] Profile builder
- [ ] Auto-capture + auto-recall
- [ ] OpenClaw plugin (TypeScript)
- [ ] ≥90% test coverage on resolver + profile

### Phase 3 — Benchmark + publish (Week 3)
- [ ] LongMemEval harness (subset, 100 questions)
- [ ] LoCoMo harness
- [ ] ConvoMem harness
- [ ] Report card generation
- [ ] README with benchmark results
- [ ] v0.1.0 release

---

## Tech Stack

| Component | Choice | Why |
|-----------|--------|-----|
| Core engine | Go | Same as EvoClaw, fast, small binary |
| Embeddings | Ollama `qwen2.5:7b` @ peter:11434 | Free, private, already running |
| Local DB | SQLite (mattn/go-sqlite3) | Zero infra |
| Cloud sync | Turso (libsql) | Already have creds, free tier |
| LLM for extraction | GLM-4.7 via proxy | ~$0.001/call, cheap |
| Plugin | TypeScript | OpenClaw plugin standard |
| Benchmark data | LongMemEval dataset (HuggingFace) | ICLR 2025, authoritative |

---

## Privacy Model

- All data stored locally first (SQLite)
- Turso sync is encrypted in transit (libsql TLS)
- Turso is the only external service — and we control the database
- Embeddings computed locally on GPU server (never leave our infra)
- LLM extraction calls go to proxy-6 (GLM-4.7) — only the extracted text, not raw conversation
- No telemetry, no analytics, no third-party SDKs

---

## Comparison vs Supermemory

| Feature | ClawMemory | Supermemory |
|---------|-----------|-------------|
| Data sovereignty | ✅ 100% ours | ❌ Their servers |
| Contradiction handling | ✅ | ✅ |
| Temporal forgetting | ✅ | ✅ |
| Hybrid search | ✅ (BM25 + Ollama) | ✅ |
| User profile | ✅ | ✅ |
| OpenClaw plugin | ✅ | ✅ |
| Benchmark suite | ✅ (LongMemEval + LoCoMo + ConvoMem) | ✅ (#1 on all 3) |
| Cost | ~$0 (free Ollama + free Turso tier) | Pro subscription |
| Setup | `go install` | `openclaw plugins install` |

**Our target:** Beat or match Supermemory on LongMemEval subset (currently #1 at ~85% recall@1).

---

## Next Step

Push this plan as `PLAN.md` to `github.com/clawinfra/clawmemory`, then spawn Builder.
