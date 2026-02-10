package main

import (
	"database/sql"
	"log"
	"os"
	"time"

	"cacc/pkg/database"
	"cacc/pkg/middleware"
	"cacc/pkg/server"
	"cacc/services/auth/handlers"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

func main() {
	db := database.Connect()
	defer db.Close()

	db.SetMaxOpenConns(15)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	setupDatabase(db)
	cleanExpiredSessions(db)

	h := handlers.New(db)

	app := server.NewApp("auth")

	// --- rotas públicas ---
	auth := app.Group("/auth")
	auth.Post("/register", limiter.New(limiter.Config{
		Max: 10, Expiration: 1 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string { return c.IP() },
	}), h.Register)

	auth.Post("/login", limiter.New(limiter.Config{
		Max: 20, Expiration: 1 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string { return c.IP() },
	}), h.Login)

	auth.Post("/refresh", h.Refresh)
	auth.Get("/session", h.Session)

	// --- rotas protegidas ---
	protected := auth.Group("", middleware.AuthMiddleware)
	protected.Get("/me", h.Me)
	protected.Post("/logout", h.Logout)
	protected.Post("/logout-all", h.LogoutAll)
	protected.Get("/sessions", h.Sessions)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}

	log.Println("Auth rodando na porta " + port)
	log.Fatal(app.Listen(":" + port))
}

func setupDatabase(db *sql.DB) {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		username TEXT UNIQUE NOT NULL,
		password TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS sessions (
		id SERIAL PRIMARY KEY,
		user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		refresh_token TEXT UNIQUE NOT NULL,
		user_agent TEXT NOT NULL DEFAULT '',
		ip TEXT NOT NULL DEFAULT '',
		expires_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
	CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(refresh_token);
	CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
	`
	if _, err := db.Exec(schema); err != nil {
		log.Fatal("Erro ao criar schema auth:", err)
	}
}

// limpeza periódica de sessões expiradas
func cleanExpiredSessions(db *sql.DB) {
	go func() {
		for {
			db.Exec(`DELETE FROM sessions WHERE expires_at < NOW()`)
			time.Sleep(1 * time.Hour)
		}
	}()
}
