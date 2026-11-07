package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/d4nyll/nodoplex/internal/api"
	"github.com/d4nyll/nodoplex/internal/changelog"
	"github.com/d4nyll/nodoplex/internal/config"
	"github.com/d4nyll/nodoplex/internal/db"
	"github.com/d4nyll/nodoplex/internal/events"
	"github.com/d4nyll/nodoplex/internal/graph"
)

func main() {
	assignNamespace := flag.String("assign-namespace", "",
		"one-off migration: move every node and changelog entry that has no namespace into this namespace, then exit")
	flag.Parse()
	if *assignNamespace != "" {
		if err := graph.ValidateNamespace(*assignNamespace); err != nil {
			log.Fatalf("assign-namespace: %v", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── Config ───────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// ── Neo4j ────────────────────────────────────────────────
	neo4jDriver, err := db.NewNeo4jDriver(ctx, cfg.Neo4jURI, cfg.Neo4jUser, cfg.Neo4jPassword)
	if err != nil {
		log.Fatalf("neo4j: %v", err)
	}
	defer neo4jDriver.Close(ctx)

	if err := db.RunNeo4jMigrations(ctx, neo4jDriver); err != nil {
		log.Fatalf("neo4j migrations: %v", err)
	}
	log.Println("neo4j: connected and migrations applied")

	// ── PostgreSQL ───────────────────────────────────────────
	pgPool, err := db.NewPostgresPool(ctx, cfg.PostgresURL)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pgPool.Close()

	if err := changelog.RunMigrations(ctx, pgPool); err != nil {
		log.Fatalf("postgres migrations: %v", err)
	}
	log.Println("postgres: connected and migrations applied")

	// ── Nodes without a namespace ────────────────────────────
	if *assignNamespace != "" {
		nodes, err := db.AssignNamespace(ctx, neo4jDriver, *assignNamespace)
		if err != nil {
			log.Fatalf("assign-namespace: %v", err)
		}
		entries, err := changelog.AssignNamespace(ctx, pgPool, *assignNamespace)
		if err != nil {
			log.Fatalf("assign-namespace: %v", err)
		}
		log.Printf("assign-namespace: moved %d nodes and %d changelog entries into namespace %q", nodes, entries, *assignNamespace)
		return
	}
	legacy, err := db.CountNodesWithoutNamespace(ctx, neo4jDriver)
	if err != nil {
		log.Fatalf("neo4j: %v", err)
	}
	if legacy > 0 {
		log.Printf("WARNING: %d nodes have no namespace and are not served by any endpoint. "+
			"Run the server once with -assign-namespace=<namespace> to move them into a namespace.", legacy)
	}

	// ── Services ─────────────────────────────────────────────
	repo := graph.NewRepository(neo4jDriver)
	graphSvc := graph.NewService(repo)
	clStore := changelog.NewStore(pgPool)
	bus := events.NewEventBus()

	// ── HTTP Handler ─────────────────────────────────────────
	handlers := api.NewHandlers(graphSvc, clStore, bus)

	// Use the generated HandlerWithOptions to register all routes
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Register generated API routes on the same mux
	apiHandler := api.NewRouter(handlers, mux)

	// Wrap with middleware
	wrapped := withCORS(withLogging(withRecovery(apiHandler)))

	// ── Server ───────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      wrapped,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // SSE needs no write timeout
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Printf("server listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
	log.Println("server stopped")
}

// ── Middleware ────────────────────────────────────────────────────────────────

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.status, time.Since(start))
	})
}

func withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic: %v", rec)
				http.Error(w, `{"message":"internal server error"}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher for SSE support.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
