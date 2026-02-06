package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/lib/pq"
)

type Sugestao struct {
	ID        int       `json:"id"`
	Texto     string    `json:"texto"`
	CreatedAt time.Time `json:"data_criacao"`
	Author    string    `json:"author"`
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
		log.Fatal(err)
	}
	defer db.Close()

	if err = db.Ping(); err != nil {
		log.Fatal("Erro Ping Banco:", err)
	}

	queryCriaTabela := `
	CREATE TABLE IF NOT EXISTS sugestoes (
		id SERIAL PRIMARY KEY,
		texto TEXT NOT NULL,
		data_criacao TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		author TEXT
	);`

	if _, err := db.Exec(queryCriaTabela); err != nil {
		log.Fatal("Erro Create Table:", err)
	}

	db.Exec(`ALTER TABLE sugestoes ADD COLUMN IF NOT EXISTS data_criacao TIMESTAMP DEFAULT CURRENT_TIMESTAMP`)
	db.Exec(`ALTER TABLE sugestoes ADD COLUMN IF NOT EXISTS author TEXT`)

	http.HandleFunc("/api/sugestoes", handleSugestoes)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("Backend CACC rodando na porta " + port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func handleSugestoes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "https://portal-cacc-frontend.vercel.app")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Cache-Control, Pragma")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		listar(w)
	} else if r.Method == http.MethodPost {
		criar(w, r)
	} else {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
	}
}

func listar(w http.ResponseWriter) {

	query := `
		SELECT id, texto, data_criacao, COALESCE(author, 'Anônimo') 
		FROM sugestoes 
		ORDER BY id DESC
	`
	rows, err := db.Query(query)
	if err != nil {
		log.Println("Erro Query:", err)
		http.Error(w, "Erro no banco", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var lista []Sugestao
	for rows.Next() {
		var s Sugestao

		if err := rows.Scan(&s.ID, &s.Texto, &s.CreatedAt, &s.Author); err != nil {
			log.Println("Erro Scan (pulando linha):", err)
			continue
		}
		lista = append(lista, s)
	}

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

	sqlStatement := `INSERT INTO sugestoes (texto, author) VALUES ($1, $2) RETURNING id`

	id := 0

	authorToSave := s.Author
	if authorToSave == "" {
		authorToSave = "Anônimo"
	}

	err := db.QueryRow(sqlStatement, s.Texto, authorToSave).Scan(&id)
	if err != nil {
		log.Println("Erro Insert:", err)
		http.Error(w, "Erro ao salvar", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "Sucesso",
		"id":     id,
	})
}
