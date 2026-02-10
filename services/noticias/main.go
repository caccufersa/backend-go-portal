package main

import (
	"database/sql"
	"log"
	"os"
	"time"

	"cacc/pkg/database"
	"cacc/pkg/server"
	"cacc/services/noticias/handlers"

	"github.com/gofiber/fiber/v2/middleware/limiter"
)

func main() {
	db := database.Connect()
	defer db.Close()

	db.SetMaxOpenConns(15)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	setupDatabase(db)

	h := handlers.New(db)

	app := server.NewApp("noticias")
	app.Use(limiter.New(limiter.Config{
		Max:        100,
		Expiration: 1 * time.Minute,
	}))

	api := app.Group("/api/noticias")

	// --- rotas públicas ---
	api.Get("/", h.Listar)
	api.Get("/destaques", h.Destaques)
	api.Get("/:id", h.BuscarPorID)

	// --- rotas protegidas (só autenticados criam/editam/deletam) ---
	editor := api.Group("", EditorAuthMiddleware)
	editor.Post("/", h.Criar)
	editor.Put("/:id", h.Atualizar)
	editor.Delete("/:id", h.Deletar)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8083"
	}

	log.Println("Notícias rodando na porta " + port)
	log.Fatal(app.Listen(":" + port))
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
	CREATE INDEX IF NOT EXISTS idx_noticias_created ON noticias(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_noticias_categoria ON noticias(categoria);
	CREATE INDEX IF NOT EXISTS idx_noticias_destaque ON noticias(destaque) WHERE destaque = true;
	`
	if _, err := db.Exec(schema); err != nil {
		log.Fatal("Erro ao criar schema noticias:", err)
	}
}
