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

	// --- Rota para resolver UUID → user (usada por outros services) ---
	app.Get("/internal/user/:uuid", func(c *fiber.Ctx) error {
		uuid := c.Params("uuid")
		var id int
		var username string
		var createdAt time.Time
		err := db.QueryRow(
			`SELECT id, username, created_at FROM users WHERE uuid = $1`, uuid,
		).Scan(&id, &username, &createdAt)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"erro": "Usuário não encontrado"})
		}
		return c.JSON(fiber.Map{
			"id": id, "uuid": uuid, "username": username, "created_at": createdAt,
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

	app.Use("/ws", func(c *fiber.Ctx) error {
		if !websocket.IsWebSocketUpgrade(c) {
			return fiber.ErrUpgradeRequired
		}

		tokenStr := c.Query("token")
		if tokenStr == "" {
			authHeader := c.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				tokenStr = authHeader[7:]
			}
		}

		userID := 0
		userUUID := ""
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
				if uid, ok := (*claims)["uuid"].(string); ok {
					userUUID = uid
				}
			}
		}

		c.Locals("user_id", userID)
		c.Locals("user_uuid", userUUID)
		return c.Next()
	})
	app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		userID, _ := c.Locals("user_id").(int)
		userUUID, _ := c.Locals("user_uuid").(string)
		wsHub.HandleClientConn(c, userID, userUUID)
	}))

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
    CREATE EXTENSION IF NOT EXISTS "pgcrypto";

    CREATE TABLE IF NOT EXISTS users (
        id SERIAL PRIMARY KEY,
        uuid UUID UNIQUE NOT NULL DEFAULT gen_random_uuid(),
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
    CREATE INDEX IF NOT EXISTS idx_users_uuid ON users(uuid);
    CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(refresh_token);
    CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
    CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
    `
	if _, err := db.Exec(schema); err != nil {
		log.Fatal("Erro ao criar schema auth:", err)
	}

	db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS uuid UUID UNIQUE DEFAULT gen_random_uuid()`)
	db.Exec(`UPDATE users SET uuid = gen_random_uuid() WHERE uuid IS NULL`)
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
