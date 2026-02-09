package main

import (
	"log"
	"os"

	"cacc/pkg/database"
	"cacc/pkg/middleware"
	"cacc/services/auth/handlers"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func main() {
	db := database.Connect()
	defer db.Close()

	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		username TEXT UNIQUE NOT NULL,
		password TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS tokens (
		id SERIAL PRIMARY KEY,
		user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token TEXT UNIQUE NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_tokens_token ON tokens(token);
	`
	if _, err := db.Exec(schema); err != nil {
		log.Fatal("Erro ao criar schema auth:", err)
	}

	h := handlers.New(db)

	app := fiber.New(fiber.Config{AppName: "CACC Auth"})
	app.Use(cors.New(middleware.CORSConfig()))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": "auth"})
	})

	app.Post("/auth/login", h.Login)
	app.Get("/auth/validate", h.ValidarToken)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}

	log.Println("Auth rodando na porta " + port)
	log.Fatal(app.Listen(":" + port))
}
