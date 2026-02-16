package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"cacc/pkg/broker"
	"cacc/pkg/database"
	"cacc/services/sugestoes/handlers"
)

func main() {
	db := database.Connect()
	defer db.Close()

	db.Exec(`
	CREATE TABLE IF NOT EXISTS sugestoes (
		id SERIAL PRIMARY KEY,
		texto TEXT NOT NULL,
		data_criacao TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		author TEXT DEFAULT 'Anônimo',
		categoria TEXT DEFAULT 'Geral'
	)`)
	db.Exec(`ALTER TABLE sugestoes ADD COLUMN IF NOT EXISTS author TEXT`)
	db.Exec(`ALTER TABLE sugestoes ADD COLUMN IF NOT EXISTS categoria TEXT DEFAULT 'Geral'`)

	b := broker.New()
	defer b.Close()

	h := handlers.New(db, b)
	h.RegisterActions()

	b.Subscribe("service:sugestoes")

	log.Println("Sugestoes worker listening on service:sugestoes")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("Sugestoes worker shutting down")
}
