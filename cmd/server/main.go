package main

import (
	"database/sql"
	"log"
	"os"
	"strings"
	"time"

	"cacc/pkg/cache"
	"cacc/pkg/database"
	"cacc/pkg/handlers"
	"cacc/pkg/hub"
	"cacc/pkg/middleware"
	"cacc/pkg/server"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/golang-jwt/jwt/v5"
)

func main() {
	db := database.Connect()
	defer db.Close()

	// Serverless PG: keep pool small, connections short-lived
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(3 * time.Minute)
	db.SetConnMaxIdleTime(30 * time.Second)

	setupDatabase(db)
	go cleanExpiredSessions(db)

	log.Println("[PORTAL] Connecting to Redis...")
	redis := cache.New()
	defer redis.Close()
	log.Println("[PORTAL] Redis connected")

	wsHub := hub.New()

	auth := handlers.NewAuth(db, wsHub, redis)
	social := handlers.NewSocial(db, wsHub, redis)
	noticias := handlers.NewNoticias(db, redis)
	sugestoes := handlers.NewSugestoes(db, redis)

	social.RegisterActions()

	app := server.NewApp("portal")

	authGroup := app.Group("/auth")
	authGroup.Post("/register", limiter.New(limiter.Config{
		Max:        5,
		Expiration: 1 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
	}), auth.Register)

	authGroup.Post("/login", limiter.New(limiter.Config{
		Max:        10,
		Expiration: 1 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
	}), auth.Login)

	authGroup.Post("/refresh", auth.Refresh)
	authGroup.Get("/session", auth.Session)

	protected := authGroup.Group("", middleware.AuthMiddleware)
	protected.Get("/me", auth.Me)
	protected.Post("/logout", auth.Logout)
	protected.Post("/logout-all", auth.LogoutAll)
	protected.Get("/sessions", auth.Sessions)

	app.Get("/hub/status", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"clients":       wsHub.ClientCount(),
			"authenticated": wsHub.AuthenticatedCount(),
		})
	})

	app.Get("/internal/user/:uuid", auth.GetUserByUUID)

	// ── Notícias REST (public read, private write) ──
	noticiasGroup := app.Group("/noticias")
	noticiasGroup.Get("/destaques", noticias.Destaques)
	noticiasGroup.Get("/:id", noticias.BuscarPorID)
	noticiasGroup.Get("/", noticias.Listar)
	noticiasPriv := noticiasGroup.Group("", middleware.AuthMiddleware)
	noticiasPriv.Post("/", noticias.Criar)
	noticiasPriv.Put("/:id", noticias.Atualizar)
	noticiasPriv.Delete("/:id", noticias.Deletar)

	// ── Sugestões REST (public read, auth write) ──
	sugestoesGroup := app.Group("/sugestoes")
	sugestoesGroup.Get("/", sugestoes.Listar)
	sugestoesPriv := sugestoesGroup.Group("", middleware.AuthMiddleware)
	sugestoesPriv.Post("/", sugestoes.Criar)

	app.Use("/ws", parseWSToken)

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

	addr := "0.0.0.0:" + port
	log.Printf("[PORTAL] WebSocket: wss://<domain>/ws")
	log.Printf("[PORTAL] Server starting on %s", addr)

	if err := app.Listen(addr); err != nil {
		log.Fatalf("[PORTAL] Failed to start: %v", err)
	}
}

func parseWSToken(c *fiber.Ctx) error {
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
			if id, ok := (*claims)["user_id"].(float64); ok {
				userID = int(id)
			}
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
}

func setupDatabase(db *sql.DB) {
	db.Exec(`CREATE EXTENSION IF NOT EXISTS "pgcrypto"`)

	schemas := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			uuid UUID UNIQUE NOT NULL DEFAULT gen_random_uuid(),
			username TEXT UNIQUE NOT NULL,
			password TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id SERIAL PRIMARY KEY,
			user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			refresh_token TEXT UNIQUE NOT NULL,
			user_agent TEXT NOT NULL DEFAULT '',
			ip TEXT NOT NULL DEFAULT '',
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS posts (
			id SERIAL PRIMARY KEY,
			texto TEXT NOT NULL,
			author TEXT NOT NULL DEFAULT 'Anônimo',
			user_id INT REFERENCES users(id) ON DELETE SET NULL,
			parent_id INT REFERENCES posts(id) ON DELETE CASCADE,
			likes INT NOT NULL DEFAULT 0,
			reply_count INT NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS noticias (
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
		)`,
		`CREATE TABLE IF NOT EXISTS sugestoes (
			id SERIAL PRIMARY KEY,
			texto TEXT NOT NULL,
			data_criacao TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			author TEXT DEFAULT 'Anônimo',
			categoria TEXT DEFAULT 'Geral'
		)`,
	}

	for _, s := range schemas {
		db.Exec(s)
	}

	alterations := []string{
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS uuid UUID UNIQUE DEFAULT gen_random_uuid()`,
		`UPDATE users SET uuid = gen_random_uuid() WHERE uuid IS NULL`,
		`ALTER TABLE posts ADD COLUMN IF NOT EXISTS user_id INT REFERENCES users(id) ON DELETE SET NULL`,
		`ALTER TABLE posts ADD COLUMN IF NOT EXISTS reply_count INT NOT NULL DEFAULT 0`,
		// Backfill reply_count for existing data
		`UPDATE posts p SET reply_count = (SELECT COUNT(*) FROM posts c WHERE c.parent_id = p.id) WHERE reply_count = 0`,
		`ALTER TABLE noticias ADD COLUMN IF NOT EXISTS tags TEXT[] DEFAULT '{}'`,
		`ALTER TABLE sugestoes ADD COLUMN IF NOT EXISTS author TEXT`,
		`ALTER TABLE sugestoes ADD COLUMN IF NOT EXISTS categoria TEXT DEFAULT 'Geral'`,
	}

	for _, a := range alterations {
		db.Exec(a)
	}

	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_users_username ON users(username)`,
		`CREATE INDEX IF NOT EXISTS idx_users_uuid ON users(uuid)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(refresh_token)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_posts_parent ON posts(parent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_posts_created ON posts(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_posts_likes ON posts(likes DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_posts_author ON posts(author)`,
		`CREATE INDEX IF NOT EXISTS idx_posts_user_id ON posts(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_posts_feed ON posts(created_at DESC) WHERE parent_id IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_posts_user_created ON posts(user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_posts_parent_created ON posts(parent_id, created_at ASC)`,
		`CREATE INDEX IF NOT EXISTS idx_posts_reply_count ON posts(reply_count) WHERE reply_count > 0`,
		`CREATE INDEX IF NOT EXISTS idx_noticias_created ON noticias(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_noticias_categoria ON noticias(categoria)`,
		`CREATE INDEX IF NOT EXISTS idx_noticias_destaque ON noticias(destaque) WHERE destaque = true`,
		`CREATE INDEX IF NOT EXISTS idx_noticias_tags ON noticias USING GIN(tags)`,
	}

	for _, idx := range indexes {
		db.Exec(idx)
	}

	log.Println("[DB] Schema initialized")
}

func cleanExpiredSessions(db *sql.DB) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		db.Exec(`DELETE FROM sessions WHERE expires_at < NOW()`)
	}
}
