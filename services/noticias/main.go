package main

import (
	"database/sql"
	"log"
	"os"
	"os/signal"
	"syscall"

	"cacc/pkg/broker"
	"cacc/pkg/database"
	"cacc/services/noticias/handlers"
)

func main() {
	db := database.Connect()
	defer db.Close()

	db.SetMaxOpenConns(15)
	db.SetMaxIdleConns(5)

	setupDatabase(db)

	log.Println("[NOTICIAS WORKER] Connecting to Redis broker...")
	b := broker.New()
	defer b.Close()
	log.Println("[NOTICIAS WORKER] Redis broker connected")

	h := handlers.New(db, b)
	h.RegisterActions()

	b.Subscribe("service:noticias")

	log.Println("[NOTICIAS WORKER] Listening on channel: service:noticias")
	log.Println("[NOTICIAS WORKER] Ready to process requests")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("Noticias worker shutting down")
}

func setupDatabase(db *sql.DB) {
	schema := `
	CREATE TABLE IF NOT EXISTS noticias (
		id SERIAL PRIMARY KEY,
		titulo TEXT NOT NULL,
		conteudo TEXT NOT NULL,
		resumo TEXT NOT NULL DEFAULT '',
		author TEXT NOT NULL DEFAULT 'Anônimo',
		categoria TEXT NOT NULL DEFAULT 'Geral',
		image_url TEXT,
		destaque BOOLEAN NOT NULL DEFAULT false,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	ALTER TABLE noticias ADD COLUMN IF NOT EXISTS tags TEXT[] DEFAULT '{}';
	CREATE INDEX IF NOT EXISTS idx_noticias_created ON noticias(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_noticias_categoria ON noticias(categoria);
	CREATE INDEX IF NOT EXISTS idx_noticias_destaque ON noticias(destaque) WHERE destaque = true;
	CREATE INDEX IF NOT EXISTS idx_noticias_tags ON noticias USING GIN(tags);
	`
	if _, err := db.Exec(schema); err != nil {
		log.Fatal("Erro ao criar schema noticias:", err)
	}
}
