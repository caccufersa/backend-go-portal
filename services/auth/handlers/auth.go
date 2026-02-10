package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"log"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"cacc/services/auth/models"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// ---------------------------------------------------------------------------
// Cache em memória para sessões ativas (evita hit no DB a cada /me)
// ---------------------------------------------------------------------------

type CachedUser struct {
	User      models.User
	ExpiresAt time.Time
}

type UserCache struct {
	mu    sync.RWMutex
	items map[int]*CachedUser
}

func NewCache() *UserCache {
	c := &UserCache{items: make(map[int]*CachedUser)}
	go c.cleanup()
	return c
}

func (c *UserCache) Get(id int) (models.User, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if item, ok := c.items[id]; ok && time.Now().Before(item.ExpiresAt) {
		return item.User, true
	}
	return models.User{}, false
}

func (c *UserCache) Set(user models.User) {
	c.mu.Lock()
	c.items[user.ID] = &CachedUser{User: user, ExpiresAt: time.Now().Add(15 * time.Minute)}
	c.mu.Unlock()
}

func (c *UserCache) Delete(id int) {
	c.mu.Lock()
	delete(c.items, id)
	c.mu.Unlock()
}

func (c *UserCache) cleanup() {
	for {
		time.Sleep(10 * time.Minute)
		c.mu.Lock()
		now := time.Now()
		for k, v := range c.items {
			if now.After(v.ExpiresAt) {
				delete(c.items, k)
			}
		}
		c.mu.Unlock()
	}
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

type Handler struct {
	DB        *sql.DB
	jwtSecret string
	cache     *UserCache
}

func New(db *sql.DB) *Handler {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "dev-secret-key-change-in-production"
	}
	return &Handler{DB: db, jwtSecret: secret, cache: NewCache()}
}

// ---------------------------------------------------------------------------
// Register
// ---------------------------------------------------------------------------

func (h *Handler) Register(c *fiber.Ctx) error {
	var req models.RegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}

	req.Username = strings.TrimSpace(req.Username)

	if err := validateUsername(req.Username); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": err.Error()})
	}
	if err := validatePassword(req.Password); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": err.Error()})
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Println("Erro hash:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro interno"})
	}

	var user models.User
	err = h.DB.QueryRow(
		`INSERT INTO users (username, password) VALUES ($1, $2)
		 RETURNING id, username, created_at`,
		strings.ToLower(req.Username), string(hashed),
	).Scan(&user.ID, &user.Username, &user.CreatedAt)

	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return c.Status(409).JSON(fiber.Map{"erro": "Username já existe"})
		}
		log.Println("Erro insert:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao criar conta"})
	}

	h.cache.Set(user)
	return h.createSessionAndRespond(c, user, 201)
}

// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

func (h *Handler) Login(c *fiber.Ctx) error {
	var req models.LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}

	if req.Username == "" || req.Password == "" {
		return c.Status(400).JSON(fiber.Map{"erro": "Username e senha obrigatórios"})
	}

	var user models.User
	var hashedPw string
	err := h.DB.QueryRow(
		`SELECT id, username, password, created_at FROM users WHERE username = $1`,
		strings.ToLower(strings.TrimSpace(req.Username)),
	).Scan(&user.ID, &user.Username, &hashedPw, &user.CreatedAt)

	if err == sql.ErrNoRows {
		return c.Status(401).JSON(fiber.Map{"erro": "Username ou senha incorretos"})
	}
	if err != nil {
		log.Println("Erro query:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro interno"})
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hashedPw), []byte(req.Password)); err != nil {
		return c.Status(401).JSON(fiber.Map{"erro": "Username ou senha incorretos"})
	}

	h.cache.Set(user)
	return h.createSessionAndRespond(c, user, 200)
}

// ---------------------------------------------------------------------------
// Refresh — rotação de refresh token (o antigo é invalidado)
// ---------------------------------------------------------------------------

func (h *Handler) Refresh(c *fiber.Ctx) error {
	// aceita do body OU do cookie
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = c.BodyParser(&req)
	if req.RefreshToken == "" {
		req.RefreshToken = c.Cookies("refresh_token")
	}
	if req.RefreshToken == "" {
		return c.Status(400).JSON(fiber.Map{"erro": "Refresh token não informado"})
	}

	// busca sessão no banco
	var session models.Session
	var user models.User
	err := h.DB.QueryRow(
		`SELECT s.id, s.user_id, s.expires_at, u.username, u.created_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.refresh_token = $1`, req.RefreshToken,
	).Scan(&session.ID, &session.UserID, &session.ExpiresAt, &user.Username, &user.CreatedAt)

	if err == sql.ErrNoRows {
		return c.Status(401).JSON(fiber.Map{"erro": "Sessão inválida ou expirada"})
	}
	if err != nil {
		log.Println("Erro refresh:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro interno"})
	}

	if time.Now().After(session.ExpiresAt) {
		h.DB.Exec(`DELETE FROM sessions WHERE id = $1`, session.ID)
		return c.Status(401).JSON(fiber.Map{"erro": "Sessão expirada, faça login novamente"})
	}

	user.ID = session.UserID

	// rotação: gera novo refresh token e atualiza a sessão existente
	newRefresh := generateRefreshToken()
	newExpiry := time.Now().Add(30 * 24 * time.Hour) // 30 dias
	_, err = h.DB.Exec(
		`UPDATE sessions SET refresh_token = $1, expires_at = $2 WHERE id = $3`,
		newRefresh, newExpiry, session.ID,
	)
	if err != nil {
		log.Println("Erro update session:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro interno"})
	}

	accessToken := h.generateAccessToken(user.ID, user.Username)
	h.setRefreshCookie(c, newRefresh, newExpiry)
	h.cache.Set(user)

	return c.JSON(models.AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: newRefresh,
		User:         user,
		ExpiresIn:    3600,
	})
}

// ---------------------------------------------------------------------------
// Session — o frontend chama ao iniciar pra verificar se continua logado
// Tenta usar o access token; se expirado, faz refresh automático via cookie
// ---------------------------------------------------------------------------

func (h *Handler) Session(c *fiber.Ctx) error {
	// 1. tenta access token
	auth := c.Get("Authorization")
	if auth != "" {
		parts := strings.Split(auth, " ")
		if len(parts) == 2 && parts[0] == "Bearer" {
			token, err := jwt.ParseWithClaims(parts[1], &jwt.MapClaims{}, func(t *jwt.Token) (interface{}, error) {
				return []byte(h.jwtSecret), nil
			})
			if err == nil && token.Valid {
				claims := token.Claims.(*jwt.MapClaims)
				userID := int((*claims)["user_id"].(float64))
				username := (*claims)["username"].(string)

				// retorna do cache se possível
				if user, ok := h.cache.Get(userID); ok {
					return c.JSON(fiber.Map{"authenticated": true, "user": user})
				}

				return c.JSON(fiber.Map{
					"authenticated": true,
					"user":          fiber.Map{"id": userID, "username": username},
				})
			}
		}
	}

	// 2. access token ausente/expirado — tenta refresh via cookie
	refreshToken := c.Cookies("refresh_token")
	if refreshToken == "" {
		return c.Status(401).JSON(fiber.Map{"authenticated": false, "erro": "Nenhuma sessão ativa"})
	}

	var session models.Session
	var user models.User
	err := h.DB.QueryRow(
		`SELECT s.id, s.user_id, s.expires_at, u.username, u.created_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.refresh_token = $1`, refreshToken,
	).Scan(&session.ID, &session.UserID, &session.ExpiresAt, &user.Username, &user.CreatedAt)

	if err != nil || time.Now().After(session.ExpiresAt) {
		if err == nil {
			h.DB.Exec(`DELETE FROM sessions WHERE id = $1`, session.ID)
		}
		h.clearRefreshCookie(c)
		return c.Status(401).JSON(fiber.Map{"authenticated": false, "erro": "Sessão expirada"})
	}

	user.ID = session.UserID

	// rotação automática
	newRefresh := generateRefreshToken()
	newExpiry := time.Now().Add(30 * 24 * time.Hour)
	h.DB.Exec(`UPDATE sessions SET refresh_token = $1, expires_at = $2 WHERE id = $3`,
		newRefresh, newExpiry, session.ID)

	accessToken := h.generateAccessToken(user.ID, user.Username)
	h.setRefreshCookie(c, newRefresh, newExpiry)
	h.cache.Set(user)

	return c.JSON(fiber.Map{
		"authenticated": true,
		"user":          user,
		"access_token":  accessToken,
		"expires_in":    3600,
	})
}

// ---------------------------------------------------------------------------
// Me — retorna dados do usuário autenticado
// ---------------------------------------------------------------------------

func (h *Handler) Me(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(int)

	if user, ok := h.cache.Get(userID); ok {
		return c.JSON(fiber.Map{"user": user})
	}

	var user models.User
	err := h.DB.QueryRow(
		`SELECT id, username, created_at FROM users WHERE id = $1`, userID,
	).Scan(&user.ID, &user.Username, &user.CreatedAt)

	if err != nil {
		return c.Status(404).JSON(fiber.Map{"erro": "Usuário não encontrado"})
	}

	h.cache.Set(user)
	return c.JSON(fiber.Map{"user": user})
}

// ---------------------------------------------------------------------------
// Logout — invalida a sessão atual
// ---------------------------------------------------------------------------

func (h *Handler) Logout(c *fiber.Ctx) error {
	refreshToken := c.Cookies("refresh_token")
	if refreshToken != "" {
		h.DB.Exec(`DELETE FROM sessions WHERE refresh_token = $1`, refreshToken)
	}

	// também tenta pelo body (mobile/SPA pode enviar)
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = c.BodyParser(&req)
	if req.RefreshToken != "" {
		h.DB.Exec(`DELETE FROM sessions WHERE refresh_token = $1`, req.RefreshToken)
	}

	userID, ok := c.Locals("user_id").(int)
	if ok {
		h.cache.Delete(userID)
	}

	h.clearRefreshCookie(c)
	return c.JSON(fiber.Map{"status": "ok"})
}

// ---------------------------------------------------------------------------
// LogoutAll — invalida todas as sessões do usuário
// ---------------------------------------------------------------------------

func (h *Handler) LogoutAll(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(int)
	h.DB.Exec(`DELETE FROM sessions WHERE user_id = $1`, userID)
	h.cache.Delete(userID)
	h.clearRefreshCookie(c)
	return c.JSON(fiber.Map{"status": "ok", "message": "Todas as sessões encerradas"})
}

// ---------------------------------------------------------------------------
// Sessions — lista sessões ativas do usuário
// ---------------------------------------------------------------------------

func (h *Handler) Sessions(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(int)

	rows, err := h.DB.Query(
		`SELECT id, user_agent, ip, expires_at, created_at FROM sessions
		 WHERE user_id = $1 AND expires_at > NOW() ORDER BY created_at DESC`, userID,
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro interno"})
	}
	defer rows.Close()

	sessions := []fiber.Map{}
	for rows.Next() {
		var s models.Session
		rows.Scan(&s.ID, &s.UserAgent, &s.IP, &s.ExpiresAt, &s.CreatedAt)
		sessions = append(sessions, fiber.Map{
			"id":         s.ID,
			"user_agent": s.UserAgent,
			"ip":         s.IP,
			"expires_at": s.ExpiresAt,
			"created_at": s.CreatedAt,
		})
	}

	return c.JSON(fiber.Map{"sessions": sessions})
}

// ===========================================================================
// Funções internas
// ===========================================================================

func (h *Handler) createSessionAndRespond(c *fiber.Ctx, user models.User, status int) error {
	accessToken := h.generateAccessToken(user.ID, user.Username)
	refreshToken := generateRefreshToken()
	expiresAt := time.Now().Add(30 * 24 * time.Hour) // 30 dias

	_, err := h.DB.Exec(
		`INSERT INTO sessions (user_id, refresh_token, user_agent, ip, expires_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		user.ID, refreshToken, c.Get("User-Agent"), c.IP(), expiresAt,
	)
	if err != nil {
		log.Println("Erro ao criar sessão:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao criar sessão"})
	}

	h.setRefreshCookie(c, refreshToken, expiresAt)

	return c.Status(status).JSON(models.AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User:         user,
		ExpiresIn:    3600,
	})
}

func (h *Handler) generateAccessToken(userID int, username string) string {
	claims := jwt.MapClaims{
		"user_id":    userID,
		"username":   username,
		"exp":        time.Now().Add(1 * time.Hour).Unix(),
		"iat":        time.Now().Unix(),
		"token_type": "access",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, _ := token.SignedString([]byte(h.jwtSecret))
	return s
}

func generateRefreshToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *Handler) setRefreshCookie(c *fiber.Ctx, token string, expires time.Time) {
	secure := os.Getenv("GO_ENV") == "production"
	sameSite := "Lax"
	if secure {
		sameSite = "None"
	}

	c.Cookie(&fiber.Cookie{
		Name:     "refresh_token",
		Value:    token,
		Expires:  expires,
		HTTPOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		Path:     "/auth",
	})
}

func (h *Handler) clearRefreshCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Expires:  time.Now().Add(-1 * time.Hour),
		HTTPOnly: true,
		Path:     "/auth",
	})
}

// ---------------------------------------------------------------------------
// Validações
// ---------------------------------------------------------------------------

func validateUsername(u string) error {
	if len(u) < 3 {
		return fiber.NewError(400, "Username deve ter ao menos 3 caracteres")
	}
	if len(u) > 30 {
		return fiber.NewError(400, "Username muito longo (max 30)")
	}
	for _, r := range u {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return fiber.NewError(400, "Username só pode ter letras, números, _ e -")
		}
	}
	return nil
}

func validatePassword(p string) error {
	if len(p) < 8 {
		return fiber.NewError(400, "Senha deve ter ao menos 8 caracteres")
	}
	if len(p) > 128 {
		return fiber.NewError(400, "Senha muito longa")
	}
	return nil
}
