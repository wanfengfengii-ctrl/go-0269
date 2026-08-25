// Command server is the runnable entry point for the silkworm egg cold-storage
// gate HTTP service. It opens a SQLite WAL database, applies the schema, runs
// the deterministic recovery scan, seeds the catalog, and starts serving the
// JSON API.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"silkworm-egg-cold-storage-gate/internal/api"
	"silkworm-egg-cold-storage-gate/internal/app"
	"silkworm-egg-cold-storage-gate/internal/catalog"
	"silkworm-egg-cold-storage-gate/internal/store"
)

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "benzhi.db"
	}

	ctx := context.Background()
	st, err := store.OpenSQLite(dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	dir := catalog.NewDirectory(catalog.SeedRules())
	svc := app.NewService(st, dir, nil, nil)
	if _, err := svc.Recover(ctx, time.Now().Unix()); err != nil {
		log.Printf("recovery scan: %v", err)
	}

	srv := NewAPIServer(svc)
	log.Printf("silkworm-egg-cold-storage-gate listening on %s (db=%s)", addr, dbPath)
	if err := http.ListenAndServe(addr, srv); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}

// NewAPIServer wires the application service into the HTTP handler. It is a
// thin indirection so the server entry point stays testable.
func NewAPIServer(svc *app.Service) http.Handler {
	return api.NewServer(svc).Handler()
}
