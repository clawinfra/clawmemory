// Package server provides the HTTP API server for ClawMemory.
// It wires together all subsystems (store, search, extractor, resolver, profile, decay)
// and exposes them via a RESTful API on the configured host:port.
package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/clawinfra/clawmemory/internal/config"
	"github.com/clawinfra/clawmemory/internal/decay"
	"github.com/clawinfra/clawmemory/internal/embed"
	"github.com/clawinfra/clawmemory/internal/extractor"
	"github.com/clawinfra/clawmemory/internal/profile"
	"github.com/clawinfra/clawmemory/internal/resolver"
	"github.com/clawinfra/clawmemory/internal/search"
	"github.com/clawinfra/clawmemory/internal/store"
)

// Server is the ClawMemory HTTP API server.
type Server struct {
	httpServer *http.Server
	store      store.Store
	embedder   *embed.Client
	searcher   *search.Searcher
	extractor  *extractor.Extractor
	resolver   *resolver.Resolver
	profile    *profile.Builder
	decay      *decay.Manager
	turso      *store.TursoSync
	startTime  time.Time
}

// New creates a Server with all dependencies wired.
func New(cfg *config.Config) (*Server, error) {
	// Initialize store
	st, err := store.NewSQLiteStore(cfg.Store.DBPath)
	if err != nil {
		return nil, fmt.Errorf("init store: %w", err)
	}

	// Initialize embedder (graceful degradation if Ollama is unavailable)
	embedder := embed.New(cfg.Embedding.OllamaURL, cfg.Embedding.Model, cfg.Embedding.Dimension)

	// Initialize extractor (only if configured)
	var ext *extractor.Extractor
	if cfg.Extractor.BaseURL != "" {
		ext = extractor.New(cfg.Extractor.BaseURL, cfg.Extractor.Model, cfg.Extractor.APIKey)
	}

	// Initialize searcher
	searcher := search.New(st, embedder, 0.4, 0.6)

	// Initialize resolver
	res := resolver.New(st, searcher, embedder)

	// Initialize profile builder
	prof := profile.New(st, ext)

	// Initialize decay manager
	pruneInterval, err := time.ParseDuration(cfg.Decay.PruneInterval)
	if err != nil {
		pruneInterval = time.Hour
	}
	decayMgr := decay.New(st, cfg.Decay.HalfLifeDays, cfg.Decay.MinImportance, pruneInterval)

	// Initialize Turso sync (optional)
	var tursoSync *store.TursoSync
	if cfg.Turso.AuthToken != "" && cfg.Turso.URL != "" {
		syncInterval, err := time.ParseDuration(cfg.Turso.SyncInterval)
		if err != nil {
			syncInterval = 5 * time.Minute
		}
		tursoSync, err = store.NewTursoSync(cfg.Store.DBPath+".turso", cfg.Turso.URL, cfg.Turso.AuthToken, syncInterval)
		if err != nil {
			// Don't fail — just log and continue without cloud sync
			tursoSync = nil
		}
	}

	srv := &Server{
		store:     st,
		embedder:  embedder,
		searcher:  searcher,
		extractor: ext,
		resolver:  res,
		profile:   prof,
		decay:     decayMgr,
		turso:     tursoSync,
		startTime: time.Now(),
	}

	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	handler := chain(mux, loggingMiddleware, corsMiddleware)

	srv.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return srv, nil
}

// NewWithStore creates a Server with a pre-built store (used in tests).
func NewWithStore(s store.Store) *Server {
	searcher := search.New(s, nil, 0.4, 0.6)
	prof := profile.New(s, nil)
	return &Server{
		store:     s,
		searcher:  searcher,
		profile:   prof,
		startTime: time.Now(),
	}
}

// Start begins listening on the configured host:port.
func (s *Server) Start() error {
	// Start background services
	if s.decay != nil {
		s.decay.Start()
	}
	if s.turso != nil {
		s.turso.Start()
	}

	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server, sync, and decay loops.
func (s *Server) Shutdown(ctx context.Context) error {
	// Stop background services
	if s.decay != nil {
		s.decay.Stop()
	}
	if s.turso != nil {
		s.turso.Stop()
		s.turso.Close()
	}

	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			return fmt.Errorf("http shutdown: %w", err)
		}
	}

	return s.store.Close()
}

// Handler returns the HTTP handler for testing without starting a server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	return chain(mux, corsMiddleware)
}
