package main

import (
	"database/sql"
	"log"
	"os"

	"cacc/pkg/database"
	"cacc/pkg/middleware"
	"cacc/services/social/handlers"
	hub "cacc/services/social/internal"
	"cacc/services/social/models"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func main() {
	db := database.Connect()
	defer db.Close()

	setupDatabase(db)

	wsHub := hub.New()
	h := handlers.New(db)

	h.OnBroadcast = func(msgType string, data interface{}) {
		wsHub.Broadcast(models.WSMessage{Type: msgType, Data: data})
	}

	app := fiber.New(fiber.Config{AppName: "CACC Social"})
	app.Use(cors.New(middleware.CORSConfig()))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "social", "clients": wsHub.ClientCount()})
	})

	api := app.Group("/api")
	api.Get("/posts", h.ListarFeed)
	api.Post("/posts", h.CriarPost)
	api.Get("/posts/:id", h.BuscarThread)
	api.Post("/posts/:id/comment", h.Comentar)
	api.Post("/posts/:id/like", h.Curtir)
	api.Delete("/posts/:id/like", h.Descurtir)

	app.Post("/internal/broadcast", func(c *fiber.Ctx) error {
		var msg models.WSMessage
		if err := c.BodyParser(&msg); err != nil {
			return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
		}
		wsHub.Broadcast(msg)
		return c.JSON(fiber.Map{"status": "ok"})
	})

	app.Use("/ws", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/ws", websocket.New(wsHub.HandleConnection))

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
	`
	if _, err := db.Exec(schema); err != nil {
		log.Fatal("Erro ao criar schema social:", err)
	}
}
