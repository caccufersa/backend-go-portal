package main

import (
	"database/sql"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cacc/pkg/broker"
	"cacc/pkg/database"
	"cacc/services/social/handlers"
)

func main() {
	db := database.Connect()
	defer db.Close()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	setupDatabase(db)

	b := broker.New()
	defer b.Close()

	h := handlers.New(db, b)
	h.RegisterActions()

	b.Subscribe("service:social")

	log.Println("Social worker listening on service:social")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Println("Social worker shutting down")
}

func setupDatabase(db *sql.DB) {
	if _, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS posts (
            id SERIAL PRIMARY KEY,
            texto TEXT NOT NULL,
            author TEXT NOT NULL DEFAULT 'Anônimo',
            parent_id INT REFERENCES posts(id) ON DELETE CASCADE,
            likes INT NOT NULL DEFAULT 0,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
        )
    `); err != nil {
		log.Println("Aviso ao criar tabela posts:", err)
	}

	indexQueries := []string{
		`CREATE INDEX IF NOT EXISTS idx_posts_parent ON posts(parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_posts_created ON posts(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_posts_likes ON posts(likes DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_posts_author ON posts(author)`,
	}
	for _, q := range indexQueries {
		if _, err := db.Exec(q); err != nil {
			log.Println("Aviso ao criar índice:", err)
		}
	}
}
