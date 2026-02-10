package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"time"

	"cacc/pkg/database"
	"cacc/pkg/middleware"
	"cacc/services/social/handlers"
	hub "cacc/services/social/internal"
	"cacc/services/social/models"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

func main() {
	db := database.Connect()
	defer db.Close()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	setupDatabase(db)

	wsHub := hub.New()
	h := handlers.New(db)

	h.OnBroadcast = func(msgType string, data interface{}) {
		wsHub.Broadcast(models.WSMessage{Type: msgType, Data: data})
	}

	app := fiber.New(fiber.Config{
		AppName:               "CACC Social",
		Prefork:               false,
		DisableStartupMessage: false,
		ReduceMemoryUsage:     true,
		JSONEncoder:           json.Marshal,
		JSONDecoder:           json.Unmarshal,
	})

	app.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed,
	}))

	app.Use(limiter.New(limiter.Config{
		Max:        100,
		Expiration: 1 * time.Minute,
	}))

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
	api.Get("/users/:username", h.BuscarPerfil)

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
	CREATE INDEX IF NOT EXISTS idx_posts_likes ON posts(likes DESC);
	CREATE INDEX IF NOT EXISTS idx_posts_author ON posts(author);
	CREATE INDEX IF NOT EXISTS idx_posts_root ON posts(created_at DESC) WHERE parent_id IS NULL;
	`
	if _, err := db.Exec(schema); err != nil {
		log.Fatal("Erro ao criar schema social:", err)
	}
}
