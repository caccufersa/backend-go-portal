package main

import (
	"database/sql"
	"log"
	"os"
	"time"

	"cacc/pkg/database"
	"cacc/pkg/hub"
	"cacc/pkg/server"
	"cacc/services/social/handlers"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

func main() {
	db := database.Connect()
	defer db.Close()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	setupDatabase(db)

	// --- Conecta ao hub central (auth) via WebSocket ---
	hubURL := os.Getenv("AUTH_HUB_URL")
	if hubURL == "" {
		hubURL = "ws://localhost:8082/ws/hub"
	}

	hubClient := hub.NewClient(hubURL, "social", []string{"*"})
	go hubClient.Connect()
	defer hubClient.Close()

	h := handlers.New(db)

	// Broadcast via hub central
	h.OnBroadcast = func(msgType string, data interface{}) {
		hubClient.Broadcast(msgType, "social", data)
	}

	app := server.NewApp("social")

	app.Use(limiter.New(limiter.Config{
		Max:        100,
		Expiration: 1 * time.Minute,
	}))

	api := app.Group("/api")
	api.Get("/posts", h.ListarFeed)
	api.Post("/posts", h.CriarPost)
	api.Get("/posts/:id", h.BuscarThread)
	api.Post("/posts/:id/comment", h.Comentar)
	api.Post("/posts/:id/like", h.Curtir)
	api.Delete("/posts/:id/like", h.Descurtir)
	api.Get("/users/:username", h.BuscarPerfil)

	// Endpoint de fallback HTTP para broadcast (mantém compatibilidade)
	app.Post("/internal/broadcast", func(c *fiber.Ctx) error {
		var msg hub.WSMessage
		if err := c.BodyParser(&msg); err != nil {
			return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
		}
		hubClient.Send(msg)
		return c.JSON(fiber.Map{"status": "ok"})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}

	log.Println("Social rodando na porta " + port)
	log.Fatal(app.Listen(":" + port))
}

func setupDatabase(db *sql.DB) {
	schema := `
	CREATE TABLE IF NOT EXISTS posts (
		id SERIAL PRIMARY KEY,
		texto TEXT NOT NULL,
		author TEXT NOT NULL DEFAULT 'Anônimo',
		parent_id INT REFERENCES posts(id) ON DELETE CASCADE,
		likes INT NOT NULL DEFAULT 0,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_posts_parent ON posts(parent_id);
	CREATE INDEX IF NOT EXISTS idx_posts_created ON posts(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_posts_likes ON posts(likes DESC);
	CREATE INDEX IF NOT EXISTS idx_posts_author ON posts(author);
	CREATE INDEX IF NOT EXISTS idx_posts_root ON posts(created_at DESC) WHERE parent_id IS NULL;
	`
	if _, err := db.Exec(schema); err != nil {
		log.Fatal("Erro ao criar schema social:", err)
	}
}
