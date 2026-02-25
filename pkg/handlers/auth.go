package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"time"

	"cacc/pkg/apperror"
	"cacc/pkg/hub"
	"cacc/pkg/models"
	"cacc/pkg/services"

	"github.com/gofiber/fiber/v2"
)

type AuthHandler struct {
	hub     *hub.Hub
	service services.AuthService
}

func NewAuth(h *hub.Hub, service services.AuthService) *AuthHandler {
	return &AuthHandler{hub: h, service: service}
}

// respondErr maps apperror.AppError → HTTP status + JSON erro field.
func respondErr(c *fiber.Ctx, err error) error {
	if ae, ok := err.(*apperror.AppError); ok {
		return c.Status(int(ae.Code)).JSON(fiber.Map{"erro": ae.Message})
	}
	return c.Status(500).JSON(fiber.Map{"erro": "Erro interno"})
}

// generateOAuthState makes a random 16-byte hex string for CSRF protection.
func generateOAuthState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ─── Register ────────────────────────────────────────────────────────────────

func (ah *AuthHandler) Register(c *fiber.Ctx) error {
	var req models.RegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}

	res, err := ah.service.Register(req, c.Get("User-Agent"), c.IP())
	if err != nil {
		return respondErr(c, err)
	}

	go ah.hub.Broadcast("user_registered", "auth", fiber.Map{
		"user_id": res.User.ID, "uuid": res.User.UUID, "username": res.User.Username,
	})

	ah.setRefreshCookie(c, res.RefreshToken, time.Now().Add(30*24*time.Hour))
	return c.Status(201).JSON(res)
}

// ─── Login ───────────────────────────────────────────────────────────────────

func (ah *AuthHandler) Login(c *fiber.Ctx) error {
	var req models.LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}

	res, err := ah.service.Login(req, c.Get("User-Agent"), c.IP())
	if err != nil {
		return respondErr(c, err)
	}

	go ah.hub.Broadcast("user_login", "auth", fiber.Map{
		"user_id": res.User.ID, "uuid": res.User.UUID, "username": res.User.Username,
	})

	ah.setRefreshCookie(c, res.RefreshToken, time.Now().Add(30*24*time.Hour))
	return c.Status(200).JSON(res)
}

// ─── Forgot Password ─────────────────────────────────────────────────────────

// POST /auth/forgot-password  body: { "email": "user@example.com" }
func (ah *AuthHandler) ForgotPassword(c *fiber.Ctx) error {
	var req models.ForgotPasswordRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}

	// Always return 200 – never reveal whether the e-mail exists (enumeration protection)
	_ = ah.service.ForgotPassword(req.Email)
	return c.JSON(fiber.Map{
		"message": "Se uma conta com esse e-mail existir, você receberá um link de redefinição em breve.",
	})
}

// ─── Reset Password ──────────────────────────────────────────────────────────

// POST /auth/reset-password  body: { "token": "...", "new_password": "..." }
func (ah *AuthHandler) ResetPassword(c *fiber.Ctx) error {
	var req models.ResetPasswordRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}

	if err := ah.service.ResetPassword(req); err != nil {
		return respondErr(c, err)
	}

	return c.JSON(fiber.Map{"message": "Senha redefinida com sucesso. Faça login novamente."})
}

// ─── Google OAuth ─────────────────────────────────────────────────────────────

// GET /auth/google → redirects to Google consent screen
func (ah *AuthHandler) GoogleLogin(c *fiber.Ctx) error {
	state := generateOAuthState()
	secure := os.Getenv("GO_ENV") == "production"
	c.Cookie(&fiber.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Expires:  time.Now().Add(10 * time.Minute),
		HTTPOnly: true,
		Secure:   secure,
		SameSite: "Lax",
		Path:     "/",
	})
	url := ah.service.GoogleOAuthURL(state)
	return c.Redirect(url, 302)
}

// GET /auth/google/callback?code=...&state=...
func (ah *AuthHandler) GoogleCallback(c *fiber.Ctx) error {
	// CSRF state check
	cookieState := c.Cookies("oauth_state")
	queryState := c.Query("state")
	if cookieState == "" || cookieState != queryState {
		return c.Status(400).JSON(fiber.Map{"erro": "state inválido (proteção CSRF)"})
	}

	code := c.Query("code")
	if code == "" {
		return c.Status(400).JSON(fiber.Map{"erro": "código OAuth ausente"})
	}

	res, err := ah.service.GoogleCallback(code, c.Get("User-Agent"), c.IP())
	if err != nil {
		return respondErr(c, err)
	}

	// Clear state cookie
	c.Cookie(&fiber.Cookie{
		Name:    "oauth_state",
		Value:   "",
		Expires: time.Now().Add(-1 * time.Hour),
		Path:    "/",
	})

	go ah.hub.Broadcast("user_login", "auth", fiber.Map{
		"user_id": res.User.ID, "uuid": res.User.UUID, "username": res.User.Username,
	})

	ah.setRefreshCookie(c, res.RefreshToken, time.Now().Add(30*24*time.Hour))

	// Redirect back to frontend with the access token in the URL fragment.
	// The frontend reads it once and discards the URL.
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}
	return c.Redirect(frontendURL+"/auth/callback#access_token="+res.AccessToken, 302)
}

// ─── Refresh ─────────────────────────────────────────────────────────────────

func (ah *AuthHandler) Refresh(c *fiber.Ctx) error {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = c.BodyParser(&req)
	if req.RefreshToken == "" {
		req.RefreshToken = c.Cookies("refresh_token")
	}

	res, err := ah.service.Refresh(req.RefreshToken)
	if err != nil {
		return respondErr(c, err)
	}

	ah.setRefreshCookie(c, res.RefreshToken, time.Now().Add(30*24*time.Hour))
	return c.JSON(res)
}

// ─── Session ─────────────────────────────────────────────────────────────────

func (ah *AuthHandler) Session(c *fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	var tokenStr string
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		tokenStr = authHeader[7:]
	}

	refreshToken := c.Cookies("refresh_token")

	res, err := ah.service.Session(tokenStr, refreshToken)
	if err != nil {
		if ae, ok := err.(*apperror.AppError); ok && ae.Code == apperror.ErrUnauthorized {
			ah.clearRefreshCookie(c)
			return c.Status(401).JSON(fiber.Map{"authenticated": false, "erro": ae.Message})
		}
		return c.Status(401).JSON(fiber.Map{"authenticated": false, "erro": "sessão inválida"})
	}

	if res.RefreshToken != "" {
		ah.setRefreshCookie(c, res.RefreshToken, time.Now().Add(30*24*time.Hour))
	}

	if res.AccessToken != "" {
		return c.JSON(fiber.Map{
			"authenticated": true,
			"user":          res.User,
			"access_token":  res.AccessToken,
			"expires_in":    res.ExpiresIn,
		})
	}
	return c.JSON(fiber.Map{"authenticated": true, "user": res.User})
}

// ─── Me ──────────────────────────────────────────────────────────────────────

func (ah *AuthHandler) Me(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(int)
	if !ok || userID <= 0 {
		return c.Status(401).JSON(fiber.Map{"erro": "Usuário não autenticado"})
	}

	user, err := ah.service.Me(userID)
	if err != nil {
		return respondErr(c, err)
	}

	return c.JSON(fiber.Map{"user": user})
}

// ─── Logout ──────────────────────────────────────────────────────────────────

func (ah *AuthHandler) Logout(c *fiber.Ctx) error {
	refreshToken := c.Cookies("refresh_token")
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = c.BodyParser(&req)
	if req.RefreshToken != "" {
		refreshToken = req.RefreshToken
	}

	userID, _ := c.Locals("user_id").(int)
	ah.service.Logout(refreshToken, userID)

	if userID > 0 {
		userUUID, _ := c.Locals("user_uuid").(string)
		go ah.hub.Broadcast("user_logout", "auth", fiber.Map{
			"user_id": userID, "uuid": userUUID,
		})
	}

	ah.clearRefreshCookie(c)
	return c.JSON(fiber.Map{"status": "ok"})
}

func (ah *AuthHandler) LogoutAll(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(int)
	if !ok || userID <= 0 {
		return c.Status(401).JSON(fiber.Map{"erro": "Usuário não autenticado"})
	}

	ah.service.LogoutAll(userID)
	ah.clearRefreshCookie(c)
	return c.JSON(fiber.Map{"status": "ok", "message": "Todas as sessões encerradas"})
}

// ─── Sessions ────────────────────────────────────────────────────────────────

func (ah *AuthHandler) Sessions(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(int)
	if !ok || userID <= 0 {
		return c.Status(401).JSON(fiber.Map{"erro": "Usuário não autenticado"})
	}

	sessions, err := ah.service.Sessions(userID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro interno"})
	}

	mappedSessions := []fiber.Map{}
	for _, s := range sessions {
		mappedSessions = append(mappedSessions, fiber.Map{
			"id": s.ID, "user_agent": s.UserAgent, "ip": s.IP,
			"expires_at": s.ExpiresAt, "created_at": s.CreatedAt,
		})
	}

	return c.JSON(fiber.Map{"sessions": mappedSessions})
}

// ─── GetUserByUUID ───────────────────────────────────────────────────────────

func (ah *AuthHandler) GetUserByUUID(c *fiber.Ctx) error {
	uuid := c.Params("uuid")

	user, err := ah.service.GetUserByUUID(uuid)
	if err != nil {
		return respondErr(c, err)
	}

	return c.JSON(fiber.Map{
		"id":         user.ID,
		"uuid":       user.UUID,
		"username":   user.Username,
		"created_at": user.CreatedAt,
	})
}

// ─── Cookie helpers ──────────────────────────────────────────────────────────

func (ah *AuthHandler) setRefreshCookie(c *fiber.Ctx, token string, expires time.Time) {
	secure := os.Getenv("GO_ENV") == "production"
	c.Cookie(&fiber.Cookie{
		Name:     "refresh_token",
		Value:    token,
		Expires:  expires,
		HTTPOnly: true,
		Secure:   secure,
		SameSite: "Lax",
		Path:     "/",
	})
}

func (ah *AuthHandler) clearRefreshCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Expires:  time.Now().Add(-1 * time.Hour),
		HTTPOnly: true,
		Path:     "/",
	})
}
