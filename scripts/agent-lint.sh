#!/bin/bash
# Agent harness linter (Go) — errors are agent-readable
set -euo pipefail
ERRORS=0
cd "$(git rev-parse --show-toplevel)"
echo "=== Agent Lint (Go) ==="

# Rule 1: No reverse dependency (internal → cmd forbidden)
echo "[1/5] Checking reverse dependencies..."
REVERSE=$(grep -rn '"github.com/clawinfra/clawmemory/cmd' internal/ 2>/dev/null || true)
if [ -n "$REVERSE" ]; then
  echo "LINT ERROR [reverse-dependency]: internal/ imports from cmd/"
  echo "$REVERSE"
  echo "  WHAT: Breaks cmd→internal→pkg layer rule."
  echo "  FIX:  Move shared logic to pkg/."
  echo "  REF:  docs/ARCHITECTURE.md#layer-rules"
  ERRORS=$((ERRORS+1))
fi

# Rule 2: All exported symbols need godoc
echo "[2/5] Checking godoc coverage..."
MISSING=$(go vet ./... 2>&1 | grep "exported.*should have comment" || true)
if [ -n "$MISSING" ]; then
  COUNT=$(echo "$MISSING" | wc -l | tr -d ' ')
  echo "LINT ERROR [missing-godoc]: $COUNT exported symbols missing godoc"
  echo "$MISSING" | head -5
  echo "  FIX:  Add // SymbolName does X comments above each exported declaration."
  echo "  REF:  docs/CONVENTIONS.md#godoc"
  ERRORS=$((ERRORS+1))
fi

# Rule 3: go build must pass
echo "[3/5] Running go build..."
if ! go build ./... 2>&1; then
  echo "LINT ERROR [build-failure]: go build ./... failed"
  echo "  FIX:  Run go build ./... and fix compile errors."
  ERRORS=$((ERRORS+1))
fi

# Rule 4: No global mutable state in internal/
echo "[4/5] Checking global state..."
GLOBAL=$(grep -rn "^var [A-Z]" internal/ 2>/dev/null | grep -v "_test.go" | grep -v "Err[A-Z]" | grep -v "embed.FS" || true)
if [ -n "$GLOBAL" ]; then
  echo "LINT WARNING [global-state]: Exported global vars found (may cause test pollution):"
  echo "$GLOBAL" | head -5
  echo "  FIX:  Move state into structs. Inject via constructors."
  echo "  REF:  docs/QUALITY.md#no-global-state"
fi

# Rule 5: AGENTS.md length
echo "[5/5] Checking AGENTS.md length..."
if [ -f AGENTS.md ] && [ "$(wc -l < AGENTS.md)" -gt 150 ]; then
  echo "LINT ERROR [agents-too-long]: AGENTS.md exceeds 150 lines"
  echo "  FIX: Move details to docs/ and replace with pointers."
  ERRORS=$((ERRORS+1))
fi

echo "=== Lint: $ERRORS error(s) ==="
[ $ERRORS -eq 0 ] || exit 1
echo "All checks passed. ✓"
