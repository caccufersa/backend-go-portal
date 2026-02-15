package main

import (
	"database/sql"
	"log"
	"os"
	"strings"
	"time"

	"cacc/pkg/database"
	"cacc/pkg/hub"
	"cacc/pkg/middleware"
	"cacc/pkg/server"
	"cacc/services/auth/handlers"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/golang-jwt/jwt/v5"
)

func main() {
	db := database.Connect()
	defer db.Close()

	db.SetMaxOpenConns(15)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	setupDatabase(db)
	cleanExpiredSessions(db)

	// --- Hub central de WebSocket ---
	wsHub := hub.New()

	h := handlers.New(db)
	// Injecta o hub no handler para broadcasts internos (login, logout, etc.)
	h.Hub = wsHub

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

	// --- Hub status (protegido) ---
	protected.Get("/hub/status", func(c *fiber.Ctx) error {
		clients, services := wsHub.ClientCount()
		return c.JSON(fiber.Map{
			"clients":  clients,
			"services": services,
		})
	})

	// --- WebSocket: microservices conectam aqui ---
	app.Use("/ws/hub", func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) {
			return c.Next()
		}
		return fiber.ErrUpgradeRequired
	})
	app.Get("/ws/hub", websocket.New(func(c *websocket.Conn) {
		wsHub.HandleServiceConn(c)
	}))

	// --- WebSocket: clientes frontend conectam aqui (com JWT) ---
	app.Use("/ws", func(c *fiber.Ctx) error {
		if !websocket.IsWebSocketUpgrade(c) {
			return fiber.ErrUpgradeRequired
		}

		// autentica via query param: /ws?token=xxx
		tokenStr := c.Query("token")
		if tokenStr == "" {
			// tenta pelo header Authorization
			authHeader := c.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				tokenStr = authHeader[7:]
			}
		}

		userID := 0
		if tokenStr != "" {
			secret := os.Getenv("JWT_SECRET")
			if secret == "" {
				secret = "dev-secret-key-change-in-production"
			}
			token, err := jwt.ParseWithClaims(tokenStr, &jwt.MapClaims{}, func(t *jwt.Token) (interface{}, error) {
				return []byte(secret), nil
			})
			if err == nil && token.Valid {
				claims := token.Claims.(*jwt.MapClaims)
				userID = int((*claims)["user_id"].(float64))
			}
		}

		c.Locals("user_id", userID)
		return c.Next()
	})
	app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		userID, _ := c.Locals("user_id").(int)
		wsHub.HandleClientConn(c, userID)
	}))

	// --- Endpoint HTTP para broadcast (fallback / interno) ---
	app.Post("/internal/broadcast", func(c *fiber.Ctx) error {
		var msg hub.WSMessage
		if err := c.BodyParser(&msg); err != nil {
			return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
		}
		wsHub.Broadcast(msg)
		return c.JSON(fiber.Map{"status": "ok"})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}

	log.Println("Auth Hub rodando na porta " + port)
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
