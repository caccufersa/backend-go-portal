package database

import (
	"database/sql"
	"log"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func Connect() *sql.DB {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		log.Println("Aviso: DATABASE_URL não definida.")
	}

	db, err := sql.Open("mysql", connStr)
	if err != nil {
		log.Fatal("Erro ao abrir conexão:", err)
	}

	if err = db.Ping(); err != nil {
		log.Fatal("Erro Ping Banco:", err)
	}

	// Configuração do Pool de Conexões
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(30 * time.Second)

	initDB(db)

	log.Println("Conexão com MariaDB estabelecida.")
	return db
}

func initDB(db *sql.DB) {
	// Schema já foi criado pelo init.sql no container
	// Este método apenas valida a conexão
	var result string
	err := db.QueryRow("SELECT VERSION()").Scan(&result)
	if err != nil {
		log.Println("Aviso: Não foi possível validar versão do banco:", err)
		return
	}
	log.Println("MariaDB versão:", result)
}
