package main

import (
	"database/sql"
	"log"
	"os"
	"strings"
	"time"

	"cacc/pkg/broker"
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

	b := broker.New()
	defer b.Close()

	wsHub := hub.New(b)

	h := handlers.New(db, b)

	app := server.NewApp("auth")

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

	protected := auth.Group("", middleware.AuthMiddleware)
	protected.Get("/me", h.Me)
	protected.Post("/logout", h.Logout)
	protected.Post("/logout-all", h.LogoutAll)
	protected.Get("/sessions", h.Sessions)

	protected.Get("/hub/status", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"clients": wsHub.ClientCount()})
	})

	api := app.Group("/api/noticias")
	api.Post("/fetch-link-meta", handlers.FetchLinkMeta)
	api.Post("/upload/image", handlers.UploadImage)

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
		username := ""
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
				if uname, ok := (*claims)["username"].(string); ok {
					username = uname
				}
			}
		}

		c.Locals("user_id", userID)
		c.Locals("user_uuid", userUUID)
		c.Locals("username", username)
		return c.Next()
	})

	app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		userID, _ := c.Locals("user_id").(int)
		userUUID, _ := c.Locals("user_uuid").(string)
		username, _ := c.Locals("username").(string)
		wsHub.HandleClientConn(c, userID, userUUID, username)
	}))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}

	log.Println("Auth Gateway rodando na porta " + port)
	log.Fatal(app.Listen(":" + port))
}

func setupDatabase(db *sql.DB) {
	if _, err := db.Exec(`CREATE EXTENSION IF NOT EXISTS "pgcrypto"`); err != nil {
		log.Println("Aviso ao criar extensão pgcrypto:", err)
	}

	if _, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS users (
            id SERIAL PRIMARY KEY,
            uuid UUID UNIQUE NOT NULL DEFAULT gen_random_uuid(),
            username TEXT UNIQUE NOT NULL,
            password TEXT NOT NULL,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
        )
    `); err != nil {
		log.Fatal("Erro ao criar tabela users:", err)
	}

	if _, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS sessions (
            id SERIAL PRIMARY KEY,
            user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
            refresh_token TEXT UNIQUE NOT NULL,
            user_agent TEXT NOT NULL DEFAULT '',
            ip TEXT NOT NULL DEFAULT '',
            expires_at TIMESTAMP NOT NULL,
            created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
        )
    `); err != nil {
		log.Fatal("Erro ao criar tabela sessions:", err)
	}

	indexQueries := []string{
		`CREATE INDEX IF NOT EXISTS idx_users_username ON users(username)`,
		`CREATE INDEX IF NOT EXISTS idx_users_uuid ON users(uuid)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(refresh_token)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at)`,
	}

	for _, indexQuery := range indexQueries {
		if _, err := db.Exec(indexQuery); err != nil {
			log.Println("Aviso ao criar índice:", err)
		}
	}

	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS uuid UUID UNIQUE DEFAULT gen_random_uuid()`); err != nil {
		log.Println("Aviso ao adicionar coluna uuid:", err)
	}
	if _, err := db.Exec(`UPDATE users SET uuid = gen_random_uuid() WHERE uuid IS NULL`); err != nil {
		log.Println("Aviso ao atualizar uuids:", err)
	}

	log.Println("Schema auth criado com sucesso")
}

func cleanExpiredSessions(db *sql.DB) {
	go func() {
		for {
			db.Exec(`DELETE FROM sessions WHERE expires_at < NOW()`)
			time.Sleep(1 * time.Hour)
		}
	}()
}
