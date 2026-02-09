package database

import (
	"database/sql"
	"log"
	"os"

	_ "github.com/lib/pq"
)

func Connect() *sql.DB {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		log.Println("Aviso: DATABASE_URL não definida.")
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Erro ao abrir conexão:", err)
	}

	if err = db.Ping(); err != nil {
		log.Fatal("Erro Ping Banco:", err)
	}

	log.Println("Conexão com PostgreSQL estabelecida.")
	return db
}
