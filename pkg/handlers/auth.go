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

	"cacc/pkg/cache"
	"cacc/pkg/hub"
	"cacc/pkg/models"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type cachedUser struct {
	User      models.User
	ExpiresAt time.Time
}

type userCache struct {
	mu     sync.RWMutex
	byID   map[int]*cachedUser
	byUUID map[string]*cachedUser
}

type AuthHandler struct {
	db        *sql.DB
	hub       *hub.Hub
	redis     *cache.Redis
	jwtSecret string
	users     *userCache
}

func NewAuth(db *sql.DB, h *hub.Hub, r *cache.Redis) *AuthHandler {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "dev-secret-key-change-in-production"
	}
	ah := &AuthHandler{
		db:        db,
		hub:       h,
		redis:     r,
		jwtSecret: secret,
		users: &userCache{
			byID:   make(map[int]*cachedUser),
			byUUID: make(map[string]*cachedUser),
		},
	}
	go ah.cleanupUsers()
	return ah
}

func (ah *AuthHandler) getUser(id int) (models.User, bool) {
	ah.users.mu.RLock()
	defer ah.users.mu.RUnlock()
	if item, ok := ah.users.byID[id]; ok && time.Now().Before(item.ExpiresAt) {
		return item.User, true
	}
	return models.User{}, false
}

func (ah *AuthHandler) setUser(user models.User) {
	ah.users.mu.Lock()
	entry := &cachedUser{User: user, ExpiresAt: time.Now().Add(15 * time.Minute)}
	ah.users.byID[user.ID] = entry
	if user.UUID != "" {
		ah.users.byUUID[user.UUID] = entry
	}
	ah.users.mu.Unlock()
}

func (ah *AuthHandler) deleteUser(id int) {
	ah.users.mu.Lock()
	if item, ok := ah.users.byID[id]; ok {
		delete(ah.users.byUUID, item.User.UUID)
	}
	delete(ah.users.byID, id)
	ah.users.mu.Unlock()
}

func (ah *AuthHandler) cleanupUsers() {
	for {
		time.Sleep(10 * time.Minute)
		ah.users.mu.Lock()
		now := time.Now()
		for k, v := range ah.users.byID {
			if now.After(v.ExpiresAt) {
				delete(ah.users.byUUID, v.User.UUID)
				delete(ah.users.byID, k)
			}
		}
		ah.users.mu.Unlock()
	}
}

func (ah *AuthHandler) Register(c *fiber.Ctx) error {
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
	err = ah.db.QueryRow(
		`INSERT INTO users (username, password) VALUES ($1, $2)
		 RETURNING id, uuid, username, created_at`,
		strings.ToLower(req.Username), string(hashed),
	).Scan(&user.ID, &user.UUID, &user.Username, &user.CreatedAt)

	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return c.Status(409).JSON(fiber.Map{"erro": "Username já existe"})
		}
		log.Println("Erro insert:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao criar conta"})
	}

	ah.setUser(user)
	go ah.hub.Broadcast("user_registered", "auth", fiber.Map{
		"user_id": user.ID, "uuid": user.UUID, "username": user.Username,
	})
	return ah.createSessionAndRespond(c, user, 201)
}

func (ah *AuthHandler) Login(c *fiber.Ctx) error {
	var req models.LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}

	if req.Username == "" || req.Password == "" {
		return c.Status(400).JSON(fiber.Map{"erro": "Username e senha obrigatórios"})
	}

	var user models.User
	var hashedPw string
	err := ah.db.QueryRow(
		`SELECT id, uuid, username, password, created_at FROM users WHERE username = $1`,
		strings.ToLower(strings.TrimSpace(req.Username)),
	).Scan(&user.ID, &user.UUID, &user.Username, &hashedPw, &user.CreatedAt)

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

	ah.setUser(user)
	go ah.hub.Broadcast("user_login", "auth", fiber.Map{
		"user_id": user.ID, "uuid": user.UUID, "username": user.Username,
	})
	return ah.createSessionAndRespond(c, user, 200)
}

func (ah *AuthHandler) Refresh(c *fiber.Ctx) error {
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

	var session models.Session
	var user models.User
	err := ah.db.QueryRow(
		`SELECT s.id, s.user_id, s.expires_at, u.uuid, u.username, u.created_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.refresh_token = $1`, req.RefreshToken,
	).Scan(&session.ID, &session.UserID, &session.ExpiresAt, &user.UUID, &user.Username, &user.CreatedAt)

	if err == sql.ErrNoRows {
		return c.Status(401).JSON(fiber.Map{"erro": "Sessão inválida ou expirada"})
	}
	if err != nil {
		log.Println("Erro refresh:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro interno"})
	}

	if time.Now().After(session.ExpiresAt) {
		ah.db.Exec(`DELETE FROM sessions WHERE id = $1`, session.ID)
		return c.Status(401).JSON(fiber.Map{"erro": "Sessão expirada, faça login novamente"})
	}

	user.ID = session.UserID

	newRefresh := generateRefreshToken()
	newExpiry := time.Now().Add(30 * 24 * time.Hour)
	_, err = ah.db.Exec(
		`UPDATE sessions SET refresh_token = $1, expires_at = $2 WHERE id = $3`,
		newRefresh, newExpiry, session.ID,
	)
	if err != nil {
		log.Println("Erro update session:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro interno"})
	}

	accessToken := ah.generateAccessToken(user)
	ah.setRefreshCookie(c, newRefresh, newExpiry)
	ah.setUser(user)

	return c.JSON(models.AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: newRefresh,
		User:         user,
		ExpiresIn:    3600,
	})
}

func (ah *AuthHandler) Session(c *fiber.Ctx) error {
	auth := c.Get("Authorization")
	if auth != "" {
		parts := strings.Split(auth, " ")
		if len(parts) == 2 && parts[0] == "Bearer" {
			token, err := jwt.ParseWithClaims(parts[1], &jwt.MapClaims{}, func(t *jwt.Token) (interface{}, error) {
				return []byte(ah.jwtSecret), nil
			})
			if err == nil && token.Valid {
				claims := token.Claims.(*jwt.MapClaims)
				userID := int((*claims)["user_id"].(float64))
				userUUID, _ := (*claims)["uuid"].(string)
				username := (*claims)["username"].(string)

				if user, ok := ah.getUser(userID); ok {
					return c.JSON(fiber.Map{"authenticated": true, "user": user})
				}

				return c.JSON(fiber.Map{
					"authenticated": true,
					"user": fiber.Map{
						"id": userID, "uuid": userUUID, "username": username,
					},
				})
			}
		}
	}

	refreshToken := c.Cookies("refresh_token")
	if refreshToken == "" {
		return c.Status(401).JSON(fiber.Map{"authenticated": false, "erro": "Nenhuma sessão ativa"})
	}

	var session models.Session
	var user models.User
	err := ah.db.QueryRow(
		`SELECT s.id, s.user_id, s.expires_at, u.uuid, u.username, u.created_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.refresh_token = $1`, refreshToken,
	).Scan(&session.ID, &session.UserID, &session.ExpiresAt, &user.UUID, &user.Username, &user.CreatedAt)

	if err != nil || time.Now().After(session.ExpiresAt) {
		if err == nil {
			ah.db.Exec(`DELETE FROM sessions WHERE id = $1`, session.ID)
		}
		ah.clearRefreshCookie(c)
		return c.Status(401).JSON(fiber.Map{"authenticated": false, "erro": "Sessão expirada"})
	}

	user.ID = session.UserID

	newRefresh := generateRefreshToken()
	newExpiry := time.Now().Add(30 * 24 * time.Hour)
	ah.db.Exec(`UPDATE sessions SET refresh_token = $1, expires_at = $2 WHERE id = $3`,
		newRefresh, newExpiry, session.ID)

	accessToken := ah.generateAccessToken(user)
	ah.setRefreshCookie(c, newRefresh, newExpiry)
	ah.setUser(user)

	return c.JSON(fiber.Map{
		"authenticated": true,
		"user":          user,
		"access_token":  accessToken,
		"expires_in":    3600,
	})
}

func (ah *AuthHandler) Me(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(int)

	if user, ok := ah.getUser(userID); ok {
		return c.JSON(fiber.Map{"user": user})
	}

	var user models.User
	err := ah.db.QueryRow(
		`SELECT id, uuid, username, created_at FROM users WHERE id = $1`, userID,
	).Scan(&user.ID, &user.UUID, &user.Username, &user.CreatedAt)

	if err != nil {
		return c.Status(404).JSON(fiber.Map{"erro": "Usuário não encontrado"})
	}

	ah.setUser(user)
	return c.JSON(fiber.Map{"user": user})
}

func (ah *AuthHandler) Logout(c *fiber.Ctx) error {
	refreshToken := c.Cookies("refresh_token")
	if refreshToken != "" {
		ah.db.Exec(`DELETE FROM sessions WHERE refresh_token = $1`, refreshToken)
	}

	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = c.BodyParser(&req)
	if req.RefreshToken != "" {
		ah.db.Exec(`DELETE FROM sessions WHERE refresh_token = $1`, req.RefreshToken)
	}

	if userID, ok := c.Locals("user_id").(int); ok {
		ah.deleteUser(userID)
		userUUID, _ := c.Locals("user_uuid").(string)
		go ah.hub.Broadcast("user_logout", "auth", fiber.Map{
			"user_id": userID, "uuid": userUUID,
		})
	}

	ah.clearRefreshCookie(c)
	return c.JSON(fiber.Map{"status": "ok"})
}

func (ah *AuthHandler) LogoutAll(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(int)
	ah.db.Exec(`DELETE FROM sessions WHERE user_id = $1`, userID)
	ah.deleteUser(userID)
	ah.clearRefreshCookie(c)
	return c.JSON(fiber.Map{"status": "ok", "message": "Todas as sessões encerradas"})
}

func (ah *AuthHandler) Sessions(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(int)

	rows, err := ah.db.Query(
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
			"id": s.ID, "user_agent": s.UserAgent, "ip": s.IP,
			"expires_at": s.ExpiresAt, "created_at": s.CreatedAt,
		})
	}

	return c.JSON(fiber.Map{"sessions": sessions})
}

func (ah *AuthHandler) GetUserByUUID(c *fiber.Ctx) error {
	uuid := c.Params("uuid")

	ah.users.mu.RLock()
	if item, ok := ah.users.byUUID[uuid]; ok && time.Now().Before(item.ExpiresAt) {
		ah.users.mu.RUnlock()
		return c.JSON(fiber.Map{
			"id": item.User.ID, "uuid": item.User.UUID,
			"username": item.User.Username, "created_at": item.User.CreatedAt,
		})
	}
	ah.users.mu.RUnlock()

	var id int
	var username string
	var createdAt time.Time
	err := ah.db.QueryRow(
		`SELECT id, username, created_at FROM users WHERE uuid = $1`, uuid,
	).Scan(&id, &username, &createdAt)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"erro": "Usuário não encontrado"})
	}
	return c.JSON(fiber.Map{"id": id, "uuid": uuid, "username": username, "created_at": createdAt})
}

func (ah *AuthHandler) createSessionAndRespond(c *fiber.Ctx, user models.User, status int) error {
	accessToken := ah.generateAccessToken(user)
	refreshToken := generateRefreshToken()
	expiresAt := time.Now().Add(30 * 24 * time.Hour)

	_, err := ah.db.Exec(
		`INSERT INTO sessions (user_id, refresh_token, user_agent, ip, expires_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		user.ID, refreshToken, c.Get("User-Agent"), c.IP(), expiresAt,
	)
	if err != nil {
		log.Println("Erro ao criar sessão:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao criar sessão"})
	}

	ah.setRefreshCookie(c, refreshToken, expiresAt)

	return c.Status(status).JSON(models.AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User:         user,
		ExpiresIn:    3600,
	})
}

func (ah *AuthHandler) generateAccessToken(user models.User) string {
	claims := jwt.MapClaims{
		"user_id":    user.ID,
		"uuid":       user.UUID,
		"username":   user.Username,
		"exp":        time.Now().Add(1 * time.Hour).Unix(),
		"iat":        time.Now().Unix(),
		"token_type": "access",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, _ := token.SignedString([]byte(ah.jwtSecret))
	return s
}

func generateRefreshToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (ah *AuthHandler) setRefreshCookie(c *fiber.Ctx, token string, expires time.Time) {
	secure := os.Getenv("GO_ENV") == "production"
	c.Cookie(&fiber.Cookie{
		Name: "refresh_token", Value: token, Expires: expires,
		HTTPOnly: true, Secure: secure, SameSite: "Lax", Path: "/",
	})
}

func (ah *AuthHandler) clearRefreshCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name: "refresh_token", Value: "", Expires: time.Now().Add(-1 * time.Hour),
		HTTPOnly: true, Path: "/",
	})
}

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
