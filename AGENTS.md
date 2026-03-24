# AGENTS.md — clawmemory Agent Harness

[One sentence describing what this repo does.]
This file is a **table of contents** — not a reference manual. Follow the links.

> **Context depth guide (progressive disclosure):**
> - **L1 (here):** orientation, commands, invariants — read this first
> - **L2 (`docs/`):** architecture, quality standards, conventions — read before coding
> - **L3 (source):** implementation details — pull on demand via grep/read tools
>
> Do not dump L2/L3 into your context unless you need it. Pull, don't pre-load.

---

## Repo Map

```
  bench/
  cmd/
  docs/
  internal/
  plugin/
  scripts/
```

---

## Packages (9 total)

```
  config
  decay
  embed
  extractor
  profile
  resolver
  search
  server
  store
```

---

## Docs (start here before touching code)

| File | What it covers |
|------|---------------|
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | Layer rules, dependency graph, key invariants |
| [`docs/QUALITY.md`](docs/QUALITY.md) | Coverage targets, security rules, testing standards |
| [`docs/CONVENTIONS.md`](docs/CONVENTIONS.md) | Naming conventions, code style |
| [`docs/EXECUTION_PLAN_TEMPLATE.md`](docs/EXECUTION_PLAN_TEMPLATE.md) | Template for planning complex tasks |

---

## How to Build & Test

```bash
# Run all tests
go test ./... -count=1 -timeout 120s

# Run lints
go vet ./...

# Run agent-specific lints (architectural invariants)
bash scripts/agent-lint.sh
```

---

## Agent Invariants (non-negotiable)

1. **Always run tests before opening a PR.** Never break existing tests.
2. **Check docs/ARCHITECTURE.md before adding cross-package dependencies.**
3. **All new public APIs must have documentation.**
4. **Run `bash scripts/agent-lint.sh` locally.** Failures include fix instructions.
5. **For complex tasks** (multiple packages, new APIs, migrations), create an execution
   plan using `docs/EXECUTION_PLAN_TEMPLATE.md` before writing code.

---

## CI Gates

Every PR runs agent-lint + tests + lints. All must pass.

---

*This file must stay under 150 lines. See `scripts/agent-lint.sh`.*