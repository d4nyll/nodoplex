package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/d4nyll/nodoplex/internal/api"
	"github.com/d4nyll/nodoplex/internal/config"
	"github.com/d4nyll/nodoplex/internal/db"
	"github.com/d4nyll/nodoplex/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	neo, err := db.NewNeo4jDriver(cfg.Neo4jURI, cfg.Neo4jUser, cfg.Neo4jPassword)
	if err != nil {
		log.Fatalf("neo4j: %v", err)
	}
	defer neo.Close(context.Background())
	pg, err := db.NewPostgresPool(ctx, cfg.PostgresURL)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pg.Close()

	st := store.New(neo, pg, store.Settings{
		ExposeChangefeed:       cfg.ExposeChangefeed,
		ChangeEntryCountTarget: cfg.ChangeEntryCountTarget,
		ChangeEntrySizeTarget:  cfg.ChangeEntrySizeTarget,
		DeleteOldChangeEntries: cfg.DeleteOldChangeEntries,
	})

	srv := &http.Server{
		Addr:              ":" + strconv.FormatInt(cfg.Port, 10),
		Handler:           withLogging(withRecovery(api.NewHandler(ctx, st))),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       60 * time.Second,
		// No WriteTimeout: a subscription stays open.
	}
	go func() {
		<-ctx.Done()
		log.Println("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()
	// The server answers the health checks while it starts (L2), so it
	// listens before it connects to the databases.
	go func() {
		log.Printf("server listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()
	// Neo4j can open its Bolt port some time after its container starts, so
	// the server tries again until it can start.
	for err := st.Start(ctx); err != nil; err = st.Start(ctx) {
		log.Printf("start: %v", err)
		select {
		case <-ctx.Done():
			log.Println("server stopped")
			return
		case <-time.After(2 * time.Second):
		}
	}
	log.Println("server started")
	st.Run(ctx)
	log.Println("server stopped")
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.status, time.Since(start))
	})
}

// withRecovery answers internal_error if a handler panics.
func withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic: %v", rec)
				w.Header().Set("Nodoplex-Error-Code", "internal_error")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"code":"internal_error","message":"the server can't complete the operation"}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController flush the event stream of a
// subscription.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
