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
	"cacc/pkg/repository"
	"cacc/pkg/server"
	"cacc/pkg/services"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/golang-jwt/jwt/v5"
)

func main() {
	// ── Secrets (read once, inject everywhere) ──────────────────────────
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "dev-secret-key-change-in-production"
		log.Println("[PORTAL] ⚠  JWT_SECRET not set – using dev default")
	}

	adminKey := os.Getenv("ADMIN_SECRET_KEY")
	if adminKey == "" {
		adminKey = "dev-admin-secret"
		log.Println("[PORTAL] ⚠  ADMIN_SECRET_KEY not set – using dev default")
	}

	// Inject secrets into middleware (called once)
	middleware.InitSecrets(jwtSecret, adminKey)

	// ── Database ────────────────────────────────────────────────────────
	db := database.Connect()
	defer db.Close()

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(3 * time.Minute)
	db.SetConnMaxIdleTime(30 * time.Second)

	go cleanExpiredSessions(db)

	// ── Redis ───────────────────────────────────────────────────────────
	log.Println("[PORTAL] Connecting to Redis...")
	redis := cache.New()
	defer redis.Close()
	log.Println("[PORTAL] Redis connected")

	// ── Hub ─────────────────────────────────────────────────────────────
	wsHub := hub.New()

	// ── Auth ────────────────────────────────────────────────────────────
	authRepo := repository.NewAuthRepository(db)
	emailSvc := services.NewEmailService()
	authService := services.NewAuthService(authRepo, emailSvc, jwtSecret)
	auth := handlers.NewAuth(wsHub, authService)

	// ── Social ──────────────────────────────────────────────────────────
	socialRepo := repository.NewSocialRepository(db)
	notifRepo := repository.NewNotificationRepository(db)
	socialService := services.NewSocialService(socialRepo, authRepo, notifRepo, redis)
	social := handlers.NewSocial(wsHub, socialService)
	notifHandler := handlers.NewNotification(notifRepo)

	// ── Notícias ────────────────────────────────────────────────────────
	noticiasRepo := repository.NewNoticiasRepository(db)
	noticiasService := services.NewNoticiasService(noticiasRepo, redis)
	noticias := handlers.NewNoticias(noticiasService)

	// ── Sugestões ───────────────────────────────────────────────────────
	sugestaoRepo := repository.NewSugestoesRepository(db)
	sugestaoService := services.NewSugestoesService(sugestaoRepo, redis)
	sugestoes := handlers.NewSugestoes(sugestaoService)

	// ── Bus ─────────────────────────────────────────────────────────────
	busRepo := repository.NewBusRepository(db)
	busService := services.NewBusService(busRepo, redis)
	bus := handlers.NewBus(busService)

	// ── Galeria ─────────────────────────────────────────────────────────
	galeriaRepo := repository.NewGaleriaRepository(db)
	galeriaService := services.NewGaleriaService(galeriaRepo)
	galeria := handlers.NewGaleria(galeriaService, socialRepo)

	// ── Fiber App ───────────────────────────────────────────────────────
	app := server.NewApp("portal")

	// ═══════════════════════════════════════════════════════════════════
	//  Routes
	// ═══════════════════════════════════════════════════════════════════

	// ── Auth routes ─────────────────────────────────────────────────────
	authGroup := app.Group("/auth")
	authGroup.Post("/register", limiter.New(limiter.Config{
		Max:        3,
		Expiration: 24 * time.Hour,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
	}), auth.Register)

	authGroup.Get("/verify-email", auth.VerifyEmail)

	authGroup.Post("/login", limiter.New(limiter.Config{
		Max:        10,
		Expiration: 1 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
	}), auth.Login)

	authGroup.Post("/forgot-password", limiter.New(limiter.Config{
		Max:        3,
		Expiration: 5 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
	}), auth.ForgotPassword)

	authGroup.Post("/reset-password", limiter.New(limiter.Config{
		Max:        5,
		Expiration: 5 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
	}), auth.ResetPassword)

	// ── Google OAuth ────────────────────────────────────────────────────
	authGroup.Get("/google", auth.GoogleLogin)
	authGroup.Get("/google/callback", auth.GoogleCallback)

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

	// ── Notícias REST (public read, admin write) ────────────────────────
	noticiasGroup := app.Group("/noticias")
	noticiasGroup.Get("/destaques", noticias.Destaques)
	noticiasGroup.Get("/:id", noticias.BuscarPorID)
	noticiasGroup.Get("/", noticias.Listar)

	noticiasAdmin := noticiasGroup.Group("", middleware.AuthMiddleware, middleware.AdminMiddleware)
	noticiasAdmin.Post("/", noticias.Criar)
	noticiasAdmin.Put("/:id", noticias.Atualizar)
	noticiasAdmin.Delete("/:id", noticias.Deletar)

	// ── Sugestões REST (public read, auth write, admin edit/delete) ─────
	sugestoesGroup := app.Group("/sugestoes")
	sugestoesGroup.Get("/", sugestoes.Listar)

	// ── Social (Feed, Threads, Profiles) ────────────────────────────────
	socialGroup := app.Group("/social")
	socialGroup.Get("/feed", middleware.OptionalAuthMiddleware, social.Feed)
	socialGroup.Get("/feed/:id", middleware.OptionalAuthMiddleware, social.Thread)
	socialGroup.Get("/profile/:username?", middleware.OptionalAuthMiddleware, social.Profile)

	socialPriv := socialGroup.Group("", middleware.AuthMiddleware)
	socialPriv.Put("/profile", social.UpdateProfile)
	socialPriv.Post("/feed", social.CreatePost)
	socialPriv.Post("/feed/:id/reply", social.CreateReply)
	socialPriv.Put("/feed/:id/like", social.LikePost)
	socialPriv.Delete("/feed/:id/like", social.UnlikePost)
	socialPriv.Delete("/feed/:id", social.DeletePost)

	sugestoesGroup.Post("/", middleware.OptionalAuthMiddleware, sugestoes.Criar)

	sugestoesAdmin := sugestoesGroup.Group("", middleware.AuthMiddleware, middleware.AdminMiddleware)
	sugestoesAdmin.Delete("/:id", sugestoes.Deletar)
	sugestoesAdmin.Put("/:id", sugestoes.Atualizar)

	// ── Bus Reserva (High Performance) ──────────────────────────────────
	busGroup := app.Group("/bus")
	busGroup.Get("/trips", bus.ListTrips)
	busGroup.Get("/:id/seats", bus.GetSeats)

	busAdmin := busGroup.Group("/trips", middleware.AuthMiddleware, middleware.AdminMiddleware)
	busAdmin.Post("/", bus.CreateTrip)
	busAdmin.Put("/:id", bus.UpdateTrip)
	busAdmin.Delete("/:id", bus.DeleteTrip)

	busPriv := busGroup.Group("", middleware.AuthMiddleware)
	busPriv.Post("/reserve", bus.Reserve)
	busPriv.Post("/cancel", bus.Cancel)
	busPriv.Get("/me", bus.MyReservations)
	busPriv.Get("/contact", bus.GetContact)
	busPriv.Put("/contact", bus.SetContact)

	notifPriv := app.Group("/notifications", middleware.AuthMiddleware)
	notifPriv.Get("/", notifHandler.GetNotifications)
	notifPriv.Put("/read", notifHandler.MarkAsRead)

	// ── Galeria (leitura pública, upload/delete autenticado) ─────────────
	galeriaGroup := app.Group("/galeria")
	galeriaGroup.Get("/list", galeria.List)
	galeriaPriv := galeriaGroup.Group("", middleware.AuthMiddleware)
	galeriaPriv.Post("/upload", galeria.Upload)
	galeriaPriv.Delete("/:id", galeria.Delete)

	// ── WebSocket ───────────────────────────────────────────────────────
	app.Use("/ws", parseWSToken(jwtSecret))

	app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		userID, _ := c.Locals("user_id").(int)
		userUUID, _ := c.Locals("user_uuid").(string)
		username, _ := c.Locals("username").(string)
		wsHub.HandleClientConn(c, userID, userUUID, username)
	}))

	// ── Start ───────────────────────────────────────────────────────────
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

// parseWSToken returns a Fiber handler that parses JWT from query or header.
// The secret is captured via closure – no os.Getenv per request.
func parseWSToken(jwtSecret string) fiber.Handler {
	secretBytes := []byte(jwtSecret)

	return func(c *fiber.Ctx) error {
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
			token, err := jwt.ParseWithClaims(tokenStr, &jwt.MapClaims{}, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fiber.ErrUnauthorized
				}
				return secretBytes, nil
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
}

func cleanExpiredSessions(db *sql.DB) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		db.Exec(`DELETE FROM sessions WHERE expires_at < NOW()`)
		db.Exec(`DELETE FROM password_reset_tokens WHERE expires_at < NOW()`)
	}
}
