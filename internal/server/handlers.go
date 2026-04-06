package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/clawinfra/clawmemory/internal/extractor"
	"github.com/clawinfra/clawmemory/internal/search"
	"github.com/clawinfra/clawmemory/internal/store"
	"github.com/google/uuid"
)

// registerRoutes sets up all HTTP routes on the mux.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/v1/ingest", s.handleIngest)
	mux.HandleFunc("/api/v1/remember", s.handleRemember)
	mux.HandleFunc("/api/v1/recall", s.handleRecall)
	mux.HandleFunc("/api/v1/profile", s.handleProfile)
	mux.HandleFunc("/api/v1/forget", s.handleForget)
	mux.HandleFunc("/api/v1/stats", s.handleStats)
	mux.HandleFunc("/api/v1/sync", s.handleSync)
	mux.HandleFunc("/api/v1/facts", s.handleFacts)
	mux.HandleFunc("/api/v1/facts/", s.handleFactByID)
}

// writeJSON encodes v as JSON and writes it to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// handleHealth handles GET /health — liveness check.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	stats, err := s.store.Stats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "stats error: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":         "ok",
		"version":        "0.2.0",
		"uptime_seconds": int(time.Since(s.startTime).Seconds()),
		"store": map[string]interface{}{
			"active_facts": stats.ActiveFacts,
			"db_size_bytes": stats.DBSizeBytes,
		},
	})
}

// ingestRequest is the request body for POST /api/v1/ingest.
type ingestRequest struct {
	SessionID string             `json:"session_id"`
	Turns     []extractor.Turn   `json:"turns"`
}

// handleIngest handles POST /api/v1/ingest — receives conversation turns, extracts facts.
func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req ingestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if len(req.Turns) == 0 {
		writeError(w, http.StatusBadRequest, "turns array is required")
		return
	}

	if req.SessionID == "" {
		req.SessionID = uuid.New().String()
	}

	ctx := r.Context()

	// Store turns
	for _, t := range req.Turns {
		turn := &store.TurnRecord{
			ID:        uuid.New().String(),
			SessionID: req.SessionID,
			Role:      t.Role,
			Content:   t.Content,
			CreatedAt: time.Now().UnixMilli(),
		}
		if err := s.store.InsertTurn(ctx, turn); err != nil {
			writeError(w, http.StatusInternalServerError, "store turn: "+err.Error())
			return
		}
	}

	// Extract facts
	var extractedFacts []extractor.Fact
	var extractErr error
	if s.extractor != nil {
		extractedFacts, extractErr = s.extractor.Extract(ctx, req.Turns)
		if extractErr != nil {
			// Log but don't fail the request — extraction is best-effort
			extractedFacts = nil
		}
	}

	// Store extracted facts + check contradictions
	type extractedFactResponse struct {
		ID         string  `json:"id"`
		Content    string  `json:"content"`
		Category   string  `json:"category"`
		Container  string  `json:"container"`
		Importance float64 `json:"importance"`
	}
	type contradictionResponse struct {
		ExistingFactID  string  `json:"existing_fact_id"`
		ExistingContent string  `json:"existing_content"`
		NewFactID       string  `json:"new_fact_id"`
		Resolution      string  `json:"resolution"`
	}

	var storedFacts []extractedFactResponse
	var contradictions []contradictionResponse

	for _, f := range extractedFacts {
		factID := uuid.New().String()
		now := time.Now().UnixMilli()
		factRecord := &store.FactRecord{
			ID:         factID,
			Content:    f.Content,
			Category:   f.Category,
			Container:  f.Container,
			Importance: f.Importance,
			Confidence: 1.0,
			Source:     req.SessionID,
			CreatedAt:  now,
			UpdatedAt:  now,
		}

		// Check for contradictions
		if s.resolver != nil {
			contrs, checkErr := s.resolver.Check(ctx, factRecord)
			if checkErr == nil {
				for _, c := range contrs {
					contradictions = append(contradictions, contradictionResponse{
						ExistingFactID:  c.ExistingFact.ID,
						ExistingContent: c.ExistingFact.Content,
						NewFactID:       factID,
						Resolution:      c.Resolution,
					})
					// Apply resolution
					s.resolver.Resolve(ctx, &c)
				}
			}
		}

		if err := s.store.InsertFact(ctx, factRecord); err == nil {
			storedFacts = append(storedFacts, extractedFactResponse{
				ID:         factID,
				Content:    f.Content,
				Category:   f.Category,
				Container:  f.Container,
				Importance: f.Importance,
			})
		}

		// Update profile incrementally
		if s.profile != nil {
			s.profile.Update(ctx, []extractor.Fact{f})
		}
	}

	// Mark turns as processed
	unprocessed, _ := s.store.GetUnprocessedTurns(ctx, 100)
	for _, t := range unprocessed {
		s.store.MarkTurnProcessed(ctx, t.ID)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"extracted_facts": storedFacts,
		"contradictions":  contradictions,
		"turns_stored":    len(req.Turns),
	})
}

// rememberRequest is the request body for POST /api/v1/remember.
type rememberRequest struct {
	Content    string  `json:"content"`
	Category   string  `json:"category"`
	Container  string  `json:"container"`
	Importance float64 `json:"importance"`
	ExpiresAt  *int64  `json:"expires_at"`
}

// handleRemember handles POST /api/v1/remember — manual fact storage.
func (s *Server) handleRemember(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req rememberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if strings.TrimSpace(req.Content) == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	// Defaults
	if req.Category == "" {
		req.Category = "general"
	}
	if req.Container == "" {
		req.Container = "general"
	}
	if req.Importance == 0 {
		req.Importance = 0.7
	}

	ctx := r.Context()
	factID := uuid.New().String()
	now := time.Now().UnixMilli()

	fact := &store.FactRecord{
		ID:         factID,
		Content:    req.Content,
		Category:   req.Category,
		Container:  req.Container,
		Importance: req.Importance,
		Confidence: 1.0,
		Source:     "manual",
		CreatedAt:  now,
		UpdatedAt:  now,
		ExpiresAt:  req.ExpiresAt,
	}

	// Check contradictions
	var contrs []map[string]interface{}
	if s.resolver != nil {
		detected, _ := s.resolver.Check(ctx, fact)
		for _, c := range detected {
			contrs = append(contrs, map[string]interface{}{
				"existing_fact_id":  c.ExistingFact.ID,
				"existing_content": c.ExistingFact.Content,
				"resolution":       c.Resolution,
			})
			s.resolver.Resolve(ctx, &c)
		}
	}

	if err := s.store.InsertFact(ctx, fact); err != nil {
		writeError(w, http.StatusInternalServerError, "store fact: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":            factID,
		"content":       req.Content,
		"category":      req.Category,
		"container":     req.Container,
		"importance":    req.Importance,
		"contradictions": contrs,
	})
}

// recallRequest is the request body for POST /api/v1/recall.
type recallRequest struct {
	Query          string  `json:"query"`
	Limit          int     `json:"limit"`
	Container      string  `json:"container"`
	Threshold      float64 `json:"threshold"`
	IncludeProfile bool    `json:"include_profile"`
}

// handleRecall handles POST /api/v1/recall — hybrid search query.
func (s *Server) handleRecall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req recallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if strings.TrimSpace(req.Query) == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	if req.Limit <= 0 {
		req.Limit = 10
	}

	ctx := r.Context()
	start := time.Now()

	results, err := s.searcher.Search(ctx, req.Query, search.SearchOpts{
		Limit:     req.Limit,
		Container: req.Container,
		Threshold: req.Threshold,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search: "+err.Error())
		return
	}

	latencyMs := time.Since(start).Milliseconds()

	var profileSummary string
	if req.IncludeProfile && s.profile != nil {
		summary, _ := s.profile.Summarize(ctx)
		profileSummary = summary
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"results":           results,
		"profile_summary":   profileSummary,
		"total_results":     len(results),
		"search_latency_ms": latencyMs,
	})
}

// handleProfile handles GET /api/v1/profile — returns user profile.
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.profile == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"entries":    map[string]string{},
			"summary":    "",
			"updated_at": 0,
		})
		return
	}

	prof, err := s.profile.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "get profile: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, prof)
}

// forgetRequest is the request body for POST /api/v1/forget.
type forgetRequest struct {
	Query     string `json:"query"`
	MaxDelete int    `json:"max_delete"`
}

// handleForget handles POST /api/v1/forget — soft-delete matching facts.
func (s *Server) handleForget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req forgetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.MaxDelete <= 0 {
		req.MaxDelete = 5
	}

	ctx := r.Context()

	// Search for matching facts
	results, err := s.searcher.Search(ctx, req.Query, search.SearchOpts{
		Limit: req.MaxDelete,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search: "+err.Error())
		return
	}

	type deletedFact struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}

	var deleted []deletedFact
	for _, result := range results {
		if err := s.store.SoftDeleteFact(ctx, result.FactID); err == nil {
			deleted = append(deleted, deletedFact{
				ID:      result.FactID,
				Content: result.Content,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"deleted_count": len(deleted),
		"deleted_facts": deleted,
	})
}

// handleStats handles GET /api/v1/stats — returns store statistics.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	stats, err := s.store.Stats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "stats: "+err.Error())
		return
	}

	lastSync, _ := s.store.LastSyncTimestamp(r.Context())

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"total_facts":        stats.TotalFacts,
		"active_facts":       stats.ActiveFacts,
		"superseded_facts":   stats.SupersededFacts,
		"deleted_facts":      stats.DeletedFacts,
		"total_turns":        stats.TotalTurns,
		"unprocessed_turns":  stats.UnprocessedTurns,
		"profile_entries":    stats.ProfileEntries,
		"db_size_bytes":      stats.DBSizeBytes,
		"embedding_dimension": 0,
		"last_sync_at":       lastSync,
	})
}

// handleSync handles POST /api/v1/sync — triggers immediate Turso sync.
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	start := time.Now()
	synced := true
	var syncErr string

	if s.turso != nil {
		if err := s.turso.SyncNow(r.Context()); err != nil {
			synced = false
			syncErr = err.Error()
		} else {
			s.store.SetLastSyncTimestamp(r.Context(), time.Now().UnixMilli())
		}
	}

	resp := map[string]interface{}{
		"synced":           synced,
		"sync_latency_ms": time.Since(start).Milliseconds(),
	}
	if syncErr != "" {
		resp["error"] = syncErr
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleFacts handles GET /api/v1/facts — list facts with filtering.
func (s *Server) handleFacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	q := r.URL.Query()
	limit := 50
	offset := 0
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	opts := store.ListFactsOpts{
		Container:         q.Get("container"),
		Category:          q.Get("category"),
		IncludeSuperseded: q.Get("include_superseded") == "true",
		IncludeDeleted:    q.Get("include_deleted") == "true",
		Limit:             limit,
		Offset:            offset,
	}

	facts, err := s.store.ListFacts(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list facts: "+err.Error())
		return
	}

	// Get total count
	total, _ := s.store.Stats(r.Context())
	totalCount := 0
	if total != nil {
		totalCount = total.TotalFacts
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"facts":  facts,
		"total":  totalCount,
		"limit":  limit,
		"offset": offset,
	})
}

// handleFactByID handles GET /api/v1/facts/{id} — get single fact.
func (s *Server) handleFactByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Extract ID from path
	path := r.URL.Path
	prefix := "/api/v1/facts/"
	if !strings.HasPrefix(path, prefix) {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}
	id := strings.TrimPrefix(path, prefix)
	if id == "" {
		// No ID → delegate to list handler
		s.handleFacts(w, r)
		return
	}

	fact, err := s.store.GetFact(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("get fact: %v", err))
		return
	}
	if fact == nil {
		writeError(w, http.StatusNotFound, "fact not found")
		return
	}

	writeJSON(w, http.StatusOK, fact)
}


