// Command infworks runs the INF/UFRGS academic works knowledge base prototype.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"infworks/internal/app"
	"infworks/internal/store"
	"infworks/internal/web"
)

func main() {
	addr := flag.String("addr", "localhost:8080", "listen address")
	dataDir := flag.String("data", "data", "directory for the SQLite database and uploaded PDFs")
	noSeed := flag.Bool("no-seed", false, "do not create demo users and works")
	flag.Parse()

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatal(err)
	}
	db, err := store.Open(filepath.Join(*dataDir, "infworks.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	docs, err := store.NewFileStore(filepath.Join(*dataDir, "documents"))
	if err != nil {
		log.Fatal(err)
	}
	if !*noSeed {
		if err := db.SeedIfEmpty(context.Background(), docs, time.Now()); err != nil {
			log.Fatal("seed: ", err)
		}
	}

	srv := web.New(
		&app.Search{Repo: db, Now: time.Now},
		&app.Cataloguing{Repo: db, Docs: docs, Now: time.Now},
		&app.Moderation{Repo: db, Now: time.Now},
		db, docs,
	)
	log.Printf("INF Works prototype listening on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, srv.Handler()))
}
