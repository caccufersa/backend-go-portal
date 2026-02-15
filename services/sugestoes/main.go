package main

import (
	"log"
	"os"

	"cacc/pkg/database"
	"cacc/pkg/hub"
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

	// --- Conecta ao hub central (auth) via WebSocket ---
	hubURL := os.Getenv("AUTH_HUB_URL")
	if hubURL == "" {
		hubURL = "ws://localhost:8082/ws/hub"
	}

	hubClient := hub.NewClient(hubURL, "sugestoes", []string{"*"})
	go hubClient.Connect()
	defer hubClient.Close()

	h := handlers.New(db)

	// Broadcast via hub central (substitui o HTTP POST ao social)
	h.OnCreate = func(s models.Sugestao) {
		hubClient.Broadcast("new_sugestao", "sugestoes", s)
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
