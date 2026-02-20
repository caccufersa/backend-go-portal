package main

import (
	"database/sql"
	"log"
	"os"
	"strings"
	"time"

	"cacc/pkg/cache"
	"cacc/pkg/database"
	"cacc/pkg/database/migrations"
	"cacc/pkg/handlers"
	"cacc/pkg/hub"
	"cacc/pkg/middleware"
	"cacc/pkg/repository"
	"cacc/pkg/server"
	"cacc/pkg/services"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pressly/goose/v3"
)

func main() {
	db := database.Connect()
	defer db.Close()

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(3 * time.Minute)
	db.SetConnMaxIdleTime(30 * time.Second)

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		log.Fatalf("[DB] goose dialect err: %v", err)
	}
	if err := goose.Up(db, "."); err != nil {
		log.Fatalf("[DB] migrations failed: %v", err)
	}
	log.Println("[DB] Schema migrations applied")
	go cleanExpiredSessions(db)

	log.Println("[PORTAL] Connecting to Redis...")
	redis := cache.New()
	defer redis.Close()
	log.Println("[PORTAL] Redis connected")

	wsHub := hub.New()

	authRepo := repository.NewAuthRepository(db)
	authService := services.NewAuthService(authRepo)
	auth := handlers.NewAuth(wsHub, authService)

	socialRepo := repository.NewSocialRepository(db)
	socialService := services.NewSocialService(socialRepo, authRepo, redis)
	_ = handlers.NewSocial(wsHub, socialService)

	noticiasRepo := repository.NewNoticiasRepository(db)
	noticiasService := services.NewNoticiasService(noticiasRepo, redis)
	noticias := handlers.NewNoticias(noticiasService)

	sugestaoRepo := repository.NewSugestoesRepository(db)
	sugestaoService := services.NewSugestoesService(sugestaoRepo, redis)
	sugestoes := handlers.NewSugestoes(sugestaoService)

	busRepo := repository.NewBusRepository(db)
	busService := services.NewBusService(busRepo, redis)
	bus := handlers.NewBus(busService)

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

	// ── Notícias REST (public read, admin write) ──
	noticiasGroup := app.Group("/noticias")
	noticiasGroup.Get("/destaques", noticias.Destaques)
	noticiasGroup.Get("/:id", noticias.BuscarPorID)
	noticiasGroup.Get("/", noticias.Listar)

	noticiasAdmin := noticiasGroup.Group("", middleware.AuthMiddleware, middleware.AdminMiddleware)
	noticiasAdmin.Post("/", noticias.Criar)
	noticiasAdmin.Put("/:id", noticias.Atualizar)
	noticiasAdmin.Delete("/:id", noticias.Deletar)

	// ── Sugestões REST (public read, auth write, admin edit/delete) ──
	sugestoesGroup := app.Group("/sugestoes")
	sugestoesGroup.Get("/", sugestoes.Listar)

	sugestoesPriv := sugestoesGroup.Group("", middleware.AuthMiddleware)
	sugestoesPriv.Post("/", sugestoes.Criar)

	sugestoesAdmin := sugestoesGroup.Group("", middleware.AuthMiddleware, middleware.AdminMiddleware)
	sugestoesAdmin.Delete("/:id", sugestoes.Deletar) // Admin can delete
	sugestoesAdmin.Put("/:id", sugestoes.Atualizar)  // Admin can update

	// ── Bus Reserva (High Performance) ──
	busGroup := app.Group("/bus")
	busGroup.Get("/trips", bus.ListTrips)    // New: public trips listing
	busGroup.Get("/:id/seats", bus.GetSeats) // Public read (fast)

	busAdmin := busGroup.Group("/trips", middleware.AuthMiddleware, middleware.AdminMiddleware)
	busAdmin.Post("/", bus.CreateTrip)      // Admin can create a trip
	busAdmin.Put("/:id", bus.UpdateTrip)    // Admin can update a trip
	busAdmin.Delete("/:id", bus.DeleteTrip) // Admin can delete a trip

	busPriv := busGroup.Group("", middleware.AuthMiddleware)
	busPriv.Post("/reserve", bus.Reserve)
	busPriv.Post("/cancel", bus.Cancel)
	busPriv.Get("/me", bus.MyReservations)

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

func cleanExpiredSessions(db *sql.DB) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		db.Exec(`DELETE FROM sessions WHERE expires_at < NOW()`)
	}
}
