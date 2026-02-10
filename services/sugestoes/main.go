package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"cacc/pkg/database"
	"cacc/pkg/server"
	"cacc/services/sugestoes/handlers"
	"cacc/services/sugestoes/models"
)

func main() {
	db := database.Connect()
	defer db.Close()

	queryCriaTabela := `
	CREATE TABLE IF NOT EXISTS sugestoes (
		id SERIAL PRIMARY KEY,
		texto TEXT NOT NULL,
		data_criacao TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		author TEXT DEFAULT 'Anônimo',
		categoria TEXT DEFAULT 'Geral'
	);`
	if _, err := db.Exec(queryCriaTabela); err != nil {
		log.Fatal("Erro Create Table:", err)
	}
	db.Exec(`ALTER TABLE sugestoes ADD COLUMN IF NOT EXISTS author TEXT`)
	db.Exec(`ALTER TABLE sugestoes ADD COLUMN IF NOT EXISTS categoria TEXT DEFAULT 'Geral'`)

	h := handlers.New(db)

	socialURL := os.Getenv("SOCIAL_SERVICE_URL")
	if socialURL == "" {
		socialURL = "http://localhost:8081"
	}

	h.OnCreate = func(s models.Sugestao) {
		msg := map[string]interface{}{
			"type":   "new_sugestao",
			"data":   s,
			"author": s.Author,
		}
		body, _ := json.Marshal(msg)
		resp, err := http.Post(socialURL+"/internal/broadcast", "application/json", bytes.NewReader(body))
		if err != nil {
			log.Println("Erro ao notificar serviço social:", err)
			return
		}
		resp.Body.Close()
	}

	app := server.NewApp("sugestoes")

	api := app.Group("/api")
	api.Get("/sugestoes", h.Listar)
	api.Post("/sugestoes", h.Criar)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("Serviço Sugestões rodando na porta " + port)
	log.Fatal(app.Listen(":" + port))
}
