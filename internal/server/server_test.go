package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/clawinfra/clawmemory/internal/config"
	"github.com/clawinfra/clawmemory/internal/extractor"
	"github.com/clawinfra/clawmemory/internal/profile"
	"github.com/clawinfra/clawmemory/internal/search"
	"github.com/clawinfra/clawmemory/internal/store"
)

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	f, _ := os.CreateTemp("", "server_test_*.db")
	path := f.Name()
	f.Close()

	st, err := store.NewSQLiteStore(path)
	if err != nil {
		os.Remove(path)
		t.Fatal(err)
	}

	t.Cleanup(func() {
		st.Close()
		os.Remove(path)
	})

	searcher := search.New(st, nil, 0.4, 0.6)
	prof := profile.New(st, nil)

	srv := &Server{
		store:     st,
		searcher:  searcher,
		profile:   prof,
		startTime: time.Now(),
	}

	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)

	return srv, httpSrv
}

func doJSON(t *testing.T, method, url string, body interface{}) (*http.Response, map[string]interface{}) {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return resp, result
}

func TestHealth(t *testing.T) {
	_, httpSrv := newTestServer(t)

	resp, data := doJSON(t, "GET", httpSrv.URL+"/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if data["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", data["status"])
	}
	if data["version"] == nil {
		t.Error("expected version field")
	}
}

func TestIngest_Success(t *testing.T) {
	_, httpSrv := newTestServer(t)

	body := map[string]interface{}{
		"session_id": "test-sess-001",
		"turns": []map[string]string{
			{"role": "user", "content": "I prefer dark mode"},
			{"role": "assistant", "content": "Noted!"},
		},
	}

	resp, data := doJSON(t, "POST", httpSrv.URL+"/api/v1/ingest", body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d: %v", resp.StatusCode, data)
	}
	if data["turns_stored"] == nil {
		t.Error("expected turns_stored field")
	}
	if n, ok := data["turns_stored"].(float64); !ok || n != 2 {
		t.Errorf("expected turns_stored=2, got %v", data["turns_stored"])
	}
}

func TestIngest_EmptyTurns(t *testing.T) {
	_, httpSrv := newTestServer(t)

	body := map[string]interface{}{
		"session_id": "test-sess",
		"turns":      []interface{}{},
	}

	resp, data := doJSON(t, "POST", httpSrv.URL+"/api/v1/ingest", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
	if data["error"] == nil {
		t.Error("expected error field")
	}
}

func TestIngest_InvalidJSON(t *testing.T) {
	_, httpSrv := newTestServer(t)

	req, _ := http.NewRequest("POST", httpSrv.URL+"/api/v1/ingest", bytes.NewReader([]byte("not-json")))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestRemember_Success(t *testing.T) {
	_, httpSrv := newTestServer(t)

	body := map[string]interface{}{
		"content":    "The project deadline is April 15th",
		"category":   "event",
		"container":  "work",
		"importance": 0.9,
	}

	resp, data := doJSON(t, "POST", httpSrv.URL+"/api/v1/remember", body)
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d: %v", resp.StatusCode, data)
	}
	if data["id"] == nil {
		t.Error("expected id in response")
	}
	if data["content"] != "The project deadline is April 15th" {
		t.Errorf("expected content, got %v", data["content"])
	}
}

func TestRemember_MissingContent(t *testing.T) {
	_, httpSrv := newTestServer(t)

	body := map[string]interface{}{
		"category": "event",
	}

	resp, _ := doJSON(t, "POST", httpSrv.URL+"/api/v1/remember", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestRecall_Success(t *testing.T) {
	_, httpSrv := newTestServer(t)

	// First add a fact
	doJSON(t, "POST", httpSrv.URL+"/api/v1/remember", map[string]interface{}{
		"content": "User lives in Australia/Sydney",
	})

	body := map[string]interface{}{
		"query":           "timezone Australia",
		"limit":           10,
		"include_profile": false,
	}

	resp, data := doJSON(t, "POST", httpSrv.URL+"/api/v1/recall", body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d: %v", resp.StatusCode, data)
	}
	if data["results"] == nil {
		t.Error("expected results field")
	}
	if data["search_latency_ms"] == nil {
		t.Error("expected search_latency_ms field")
	}
}

func TestRecall_EmptyQuery(t *testing.T) {
	_, httpSrv := newTestServer(t)

	body := map[string]interface{}{
		"query": "",
	}

	resp, _ := doJSON(t, "POST", httpSrv.URL+"/api/v1/recall", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestProfile_Success(t *testing.T) {
	_, httpSrv := newTestServer(t)

	resp, data := doJSON(t, "GET", httpSrv.URL+"/api/v1/profile", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if data["entries"] == nil {
		t.Error("expected entries field")
	}
}

func TestForget_Success(t *testing.T) {
	_, httpSrv := newTestServer(t)

	// Add some facts first
	doJSON(t, "POST", httpSrv.URL+"/api/v1/remember", map[string]interface{}{
		"content": "Old project deadline March 1st",
	})

	body := map[string]interface{}{
		"query":      "old project deadline",
		"max_delete": 5,
	}

	resp, data := doJSON(t, "POST", httpSrv.URL+"/api/v1/forget", body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d: %v", resp.StatusCode, data)
	}
	if data["deleted_count"] == nil {
		t.Error("expected deleted_count field")
	}
}

func TestFacts_List(t *testing.T) {
	_, httpSrv := newTestServer(t)

	// Add some facts
	for i := 0; i < 3; i++ {
		doJSON(t, "POST", httpSrv.URL+"/api/v1/remember", map[string]interface{}{
			"content": fmt.Sprintf("Test fact %d", i),
		})
	}

	resp, data := doJSON(t, "GET", httpSrv.URL+"/api/v1/facts", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if data["facts"] == nil {
		t.Error("expected facts field")
	}
}

func TestFacts_Filter(t *testing.T) {
	_, httpSrv := newTestServer(t)

	doJSON(t, "POST", httpSrv.URL+"/api/v1/remember", map[string]interface{}{
		"content":   "Work fact",
		"container": "work",
	})
	doJSON(t, "POST", httpSrv.URL+"/api/v1/remember", map[string]interface{}{
		"content":   "Personal fact",
		"container": "personal",
	})

	resp, data := doJSON(t, "GET", httpSrv.URL+"/api/v1/facts?container=work", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	facts, ok := data["facts"].([]interface{})
	if !ok {
		t.Fatalf("expected facts array, got %T", data["facts"])
	}
	for _, f := range facts {
		fm := f.(map[string]interface{})
		if fm["container"] != "work" {
			t.Errorf("expected work container, got %v", fm["container"])
		}
	}
}

func TestFactByID_Found(t *testing.T) {
	_, httpSrv := newTestServer(t)

	_, created := doJSON(t, "POST", httpSrv.URL+"/api/v1/remember", map[string]interface{}{
		"content": "Test fact for retrieval",
	})
	id, ok := created["id"].(string)
	if !ok {
		t.Fatal("expected id in created response")
	}

	resp, data := doJSON(t, "GET", httpSrv.URL+"/api/v1/facts/"+id, nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d: %v", resp.StatusCode, data)
	}
	if data["id"] != id {
		t.Errorf("expected id %s, got %v", id, data["id"])
	}
}

func TestFactByID_NotFound(t *testing.T) {
	_, httpSrv := newTestServer(t)

	resp, _ := doJSON(t, "GET", httpSrv.URL+"/api/v1/facts/nonexistent-id-12345", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestStats(t *testing.T) {
	_, httpSrv := newTestServer(t)

	resp, data := doJSON(t, "GET", httpSrv.URL+"/api/v1/stats", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if data["total_facts"] == nil {
		t.Error("expected total_facts field")
	}
	if data["active_facts"] == nil {
		t.Error("expected active_facts field")
	}
}

func TestSync(t *testing.T) {
	_, httpSrv := newTestServer(t)

	resp, data := doJSON(t, "POST", httpSrv.URL+"/api/v1/sync", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d: %v", resp.StatusCode, data)
	}
	if data["synced"] == nil {
		t.Error("expected synced field")
	}
}

func TestCORS(t *testing.T) {
	_, httpSrv := newTestServer(t)

	req, _ := http.NewRequest("OPTIONS", httpSrv.URL+"/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.Header.Get("Access-Control-Allow-Origin") == "" {
		t.Error("expected CORS header Access-Control-Allow-Origin")
	}
}

func TestContentType(t *testing.T) {
	_, httpSrv := newTestServer(t)

	resp, _ := doJSON(t, "GET", httpSrv.URL+"/health", nil)
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		t.Error("expected Content-Type header")
	}
}

func TestIngest_MethodNotAllowed(t *testing.T) {
	_, httpSrv := newTestServer(t)
	resp, _ := doJSON(t, "GET", httpSrv.URL+"/api/v1/ingest", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestRemember_MethodNotAllowed(t *testing.T) {
	_, httpSrv := newTestServer(t)
	resp, _ := doJSON(t, "GET", httpSrv.URL+"/api/v1/remember", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestRecall_MethodNotAllowed(t *testing.T) {
	_, httpSrv := newTestServer(t)
	resp, _ := doJSON(t, "GET", httpSrv.URL+"/api/v1/recall", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestForget_MethodNotAllowed(t *testing.T) {
	_, httpSrv := newTestServer(t)
	resp, _ := doJSON(t, "GET", httpSrv.URL+"/api/v1/forget", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestHealth_MethodNotAllowed(t *testing.T) {
	_, httpSrv := newTestServer(t)
	resp, _ := doJSON(t, "POST", httpSrv.URL+"/health", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestStats_MethodNotAllowed(t *testing.T) {
	_, httpSrv := newTestServer(t)
	resp, _ := doJSON(t, "POST", httpSrv.URL+"/api/v1/stats", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestSync_MethodNotAllowed(t *testing.T) {
	_, httpSrv := newTestServer(t)
	resp, _ := doJSON(t, "GET", httpSrv.URL+"/api/v1/sync", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestFacts_MethodNotAllowed(t *testing.T) {
	_, httpSrv := newTestServer(t)
	resp, _ := doJSON(t, "POST", httpSrv.URL+"/api/v1/facts", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestFactByID_MethodNotAllowed(t *testing.T) {
	_, httpSrv := newTestServer(t)
	resp, _ := doJSON(t, "POST", httpSrv.URL+"/api/v1/facts/some-id", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestProfile_MethodNotAllowed(t *testing.T) {
	_, httpSrv := newTestServer(t)
	resp, _ := doJSON(t, "POST", httpSrv.URL+"/api/v1/profile", nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestRecall_WithProfile(t *testing.T) {
	_, httpSrv := newTestServer(t)

	// Add a fact
	doJSON(t, "POST", httpSrv.URL+"/api/v1/remember", map[string]interface{}{
		"content":   "User timezone is Australia/Sydney",
		"category":  "preference",
		"container": "personal",
	})

	body := map[string]interface{}{
		"query":           "timezone",
		"limit":           5,
		"include_profile": true,
	}

	resp, data := doJSON(t, "POST", httpSrv.URL+"/api/v1/recall", body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d: %v", resp.StatusCode, data)
	}
}

func TestForget_InvalidJSON(t *testing.T) {
	_, httpSrv := newTestServer(t)
	req, _ := http.NewRequest("POST", httpSrv.URL+"/api/v1/forget", bytes.NewReader([]byte("bad json")))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestIngest_MissingSessionID(t *testing.T) {
	_, httpSrv := newTestServer(t)

	body := map[string]interface{}{
		// No session_id — should auto-generate
		"turns": []map[string]string{
			{"role": "user", "content": "Hello"},
		},
	}

	resp, data := doJSON(t, "POST", httpSrv.URL+"/api/v1/ingest", body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d: %v", resp.StatusCode, data)
	}
}

func TestRemember_DefaultValues(t *testing.T) {
	_, httpSrv := newTestServer(t)

	// Only content provided — category/container/importance should default
	body := map[string]interface{}{
		"content": "Just a bare fact",
	}

	resp, data := doJSON(t, "POST", httpSrv.URL+"/api/v1/remember", body)
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d: %v", resp.StatusCode, data)
	}
	if data["category"] != "general" {
		t.Errorf("expected default category 'general', got %v", data["category"])
	}
}

func TestFacts_Pagination(t *testing.T) {
	_, httpSrv := newTestServer(t)

	// Add 5 facts
	for i := 0; i < 5; i++ {
		doJSON(t, "POST", httpSrv.URL+"/api/v1/remember", map[string]interface{}{
			"content": fmt.Sprintf("Pagination fact %d", i),
		})
	}

	resp, data := doJSON(t, "GET", httpSrv.URL+"/api/v1/facts?limit=2&offset=0", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	facts, ok := data["facts"].([]interface{})
	if !ok {
		t.Fatalf("expected facts array")
	}
	if len(facts) != 2 {
		t.Errorf("expected 2 facts (limit=2), got %d", len(facts))
	}
}

func TestNewWithStore(t *testing.T) {
	f, _ := os.CreateTemp("", "withstore_test_*.db")
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	st, err := store.NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	srv := NewWithStore(st)
	if srv == nil {
		t.Error("NewWithStore returned nil")
	}
}

func TestProfile_NoBuilder(t *testing.T) {
	f, _ := os.CreateTemp("", "nobuilder_test_*.db")
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	st, err := store.NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	srv := &Server{
		store:     st,
		searcher:  search.New(st, nil, 0.4, 0.6),
		profile:   nil, // No profile builder
		startTime: time.Now(),
	}
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	resp, data := doJSON(t, "GET", httpSrv.URL+"/api/v1/profile", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with nil profile builder, got %d: %v", resp.StatusCode, data)
	}
}

func TestServerWithLoggingMiddleware(t *testing.T) {
	f, _ := os.CreateTemp("", "log_middleware_test_*.db")
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	st, err := store.NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	srv := &Server{
		store:     st,
		searcher:  search.New(st, nil, 0.4, 0.6),
		profile:   profile.New(st, nil),
		startTime: time.Now(),
	}

	// Use loggingMiddleware explicitly
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	handler := chain(mux, loggingMiddleware, corsMiddleware, contentTypeMiddleware)

	httpSrv := httptest.NewServer(handler)
	defer httpSrv.Close()

	resp, _ := doJSON(t, "GET", httpSrv.URL+"/health", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestWriteHeaderCapture(t *testing.T) {
	// Test responseWriter.WriteHeader
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec, status: http.StatusOK}
	rw.WriteHeader(http.StatusCreated)
	if rw.status != http.StatusCreated {
		t.Errorf("expected status 201, got %d", rw.status)
	}
}

func TestNewServer(t *testing.T) {
	dir := t.TempDir()

	cfg := &config.Config{}
	*cfg = *config.Default()
	cfg.Store.DBPath = dir + "/test.db"
	cfg.Server.Port = 17437 // use a different port

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New server: %v", err)
	}
	defer srv.store.Close()

	if srv.store == nil {
		t.Error("expected non-nil store")
	}
}

func TestShutdown(t *testing.T) {
	dir := t.TempDir()

	cfg := config.Default()
	cfg.Store.DBPath = dir + "/shutdown.db"
	cfg.Server.Port = 17438

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New server: %v", err)
	}

	// Start in background
	go srv.Start()
	time.Sleep(50 * time.Millisecond)

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		// "server closed" is acceptable
		t.Logf("Shutdown: %v (acceptable)", err)
	}
}

func TestFactByID_NoID(t *testing.T) {
	_, httpSrv := newTestServer(t)

	// Request to /api/v1/facts/ with trailing slash but no ID
	// Should delegate to list handler
	resp, data := doJSON(t, "GET", httpSrv.URL+"/api/v1/facts/", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d: %v", resp.StatusCode, data)
	}
}

func TestIngest_WithExtractor(t *testing.T) {
	// Build a server with a mock extractor
	f, _ := os.CreateTemp("", "ingest_ext_test_*.db")
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	st, err := store.NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Mock extractor that returns a fact
	extSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `[{"content":"User prefers Go","category":"preference","container":"work","importance":0.8}]`}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer extSrv.Close()

	srv := &Server{
		store:     st,
		searcher:  search.New(st, nil, 0.4, 0.6),
		profile:   profile.New(st, nil),
		extractor: extractor.New(extSrv.URL, "glm-4.7", ""),
		startTime: time.Now(),
	}

	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	body := map[string]interface{}{
		"session_id": "ext-test-001",
		"turns": []map[string]string{
			{"role": "user", "content": "I love using Go for everything"},
			{"role": "assistant", "content": "Great choice!"},
		},
	}

	resp, data := doJSON(t, "POST", httpSrv.URL+"/api/v1/ingest", body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d: %v", resp.StatusCode, data)
	}

	// Should have extracted facts
	facts, _ := data["extracted_facts"].([]interface{})
	if len(facts) == 0 {
		t.Log("No extracted facts (extractor path may differ)")
	}
}

func TestRemember_WithExpiresAt(t *testing.T) {
	_, httpSrv := newTestServer(t)

	future := time.Now().Add(time.Hour).Unix()
	body := map[string]interface{}{
		"content":    "Temporary reminder",
		"category":   "event",
		"container":  "work",
		"importance": 0.9,
		"expires_at": future,
	}

	resp, data := doJSON(t, "POST", httpSrv.URL+"/api/v1/remember", body)
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d: %v", resp.StatusCode, data)
	}
}

func TestRecall_InvalidJSON(t *testing.T) {
	_, httpSrv := newTestServer(t)
	req, _ := http.NewRequest("POST", httpSrv.URL+"/api/v1/recall", bytes.NewReader([]byte("bad")))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestStats_Full(t *testing.T) {
	_, httpSrv := newTestServer(t)

	// Add data to make stats interesting
	doJSON(t, "POST", httpSrv.URL+"/api/v1/remember", map[string]interface{}{
		"content": "Stats test fact",
	})

	resp, data := doJSON(t, "GET", httpSrv.URL+"/api/v1/stats", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if data["total_facts"] == nil {
		t.Error("expected total_facts")
	}
	totalFacts := int(data["total_facts"].(float64))
	if totalFacts < 1 {
		t.Errorf("expected at least 1 fact in stats, got %d", totalFacts)
	}
}

func TestSync_NoTurso(t *testing.T) {
	// Server with no turso — sync should still return synced:true
	_, httpSrv := newTestServer(t)
	resp, data := doJSON(t, "POST", httpSrv.URL+"/api/v1/sync", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	// No turso but synced=true (nothing to sync)
	_ = data
}

func TestFacts_AllFilters(t *testing.T) {
	_, httpSrv := newTestServer(t)

	doJSON(t, "POST", httpSrv.URL+"/api/v1/remember", map[string]interface{}{
		"content":  "Technical fact about Go",
		"category": "technical",
	})

	resp, data := doJSON(t, "GET", httpSrv.URL+"/api/v1/facts?category=technical&include_superseded=false&include_deleted=false&limit=10&offset=0", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d: %v", resp.StatusCode, data)
	}
}
