package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"

	_ "github.com/lib/pq"
)

type Sugestao struct {
	ID    int    `json:"id"`
	Texto string `json:"texto"`
	CreateAt string `json:"data_criacao"`
	Author string `json:"author"`
}

var db *sql.DB

func main() {
	
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		log.Fatal("Erro: A variável de ambiente DATABASE_URL é obrigatória.")
	}

	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Erro ao abrir conexão:", err)
	}
	defer db.Close()

	if err = db.Ping(); err != nil {
		log.Fatal("Erro ao conectar no banco (Ping):", err)
	}

	queryCriaTabela := `
	CREATE TABLE IF NOT EXISTS sugestoes (
		id SERIAL PRIMARY KEY,
		texto TEXT NOT NULL,
		data_criacao TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		author TEXT NOT NULL
	);`
	
	if _, err := db.Exec(queryCriaTabela); err != nil {
		log.Fatal("Erro ao criar tabela:", err)
	}

	http.HandleFunc("/api/sugestoes", handleSugestoes)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("Backend CACC rodando")
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleSugestoes(w http.ResponseWriter, r *http.Request) {
	// CORS (Permite que seu Next.js acesse o backend)
	w.Header().Set("Access-Control-Allow-Origin", "*") 
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		listar(w)
	case http.MethodPost:
		criar(w, r)
	default:
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
	}
}

func listar(w http.ResponseWriter) {
	rows, err := db.Query("SELECT id, texto, data_criacao, author FROM sugestoes ORDER BY id DESC")
	if err != nil {
		http.Error(w, "Erro no banco", http.StatusInternalServerError)
		log.Println("Erro query:", err)
		return
	}
	defer rows.Close()

	var lista []Sugestao
	for rows.Next() {
		var s Sugestao
		if err := rows.Scan(&s.ID, &s.Texto, &s.CreateAt, &s.Author); err != nil {
			continue
		}
		lista = append(lista, s)
	}
	
	// Retorna array vazio [] em vez de null se não tiver nada
	if lista == nil {
		lista = []Sugestao{}
	}
	json.NewEncoder(w).Encode(lista)
}

func criar(w http.ResponseWriter, r *http.Request) {
	var s Sugestao
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		http.Error(w, "JSON inválido", http.StatusBadRequest)
		return
	}

	sqlStatement := `INSERT INTO sugestoes (texto, data_criacao, author) VALUES ($1, $2, $3) RETURNING id`
	id := 0
	err := db.QueryRow(sqlStatement, s.Texto, s.CreateAt, s.Author).Scan(&id)
	if err != nil {
		http.Error(w, "Erro ao salvar", http.StatusInternalServerError)
		log.Println("Erro insert:", err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "Sucesso",
		"id":     id,
	})
}