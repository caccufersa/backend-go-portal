package handlers

import (
	"os"
	"time"

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
	return &AuthHandler{
		hub:     h,
		service: service,
	}
}

func (ah *AuthHandler) Register(c *fiber.Ctx) error {
	var req models.RegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}

	res, err := ah.service.Register(req, c.Get("User-Agent"), c.IP())
	if err != nil {
		if err.Error() == "username já existe" {
			return c.Status(409).JSON(fiber.Map{"erro": err.Error()})
		}
		if err.Error() == "username deve ter ao menos 3 caracteres" ||
			err.Error() == "username muito longo (max 30)" ||
			err.Error() == "username só pode ter letras, números, _ e -" ||
			err.Error() == "senha deve ter ao menos 8 caracteres" ||
			err.Error() == "senha muito longa" {
			return c.Status(400).JSON(fiber.Map{"erro": err.Error()})
		}
		return c.Status(500).JSON(fiber.Map{"erro": err.Error()})
	}

	go ah.hub.Broadcast("user_registered", "auth", fiber.Map{
		"user_id": res.User.ID, "uuid": res.User.UUID, "username": res.User.Username,
	})

	ah.setRefreshCookie(c, res.RefreshToken, time.Now().Add(30*24*time.Hour))
	return c.Status(201).JSON(res)
}

func (ah *AuthHandler) Login(c *fiber.Ctx) error {
	var req models.LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}

	res, err := ah.service.Login(req, c.Get("User-Agent"), c.IP())
	if err != nil {
		if err.Error() == "username e senha obrigatórios" {
			return c.Status(400).JSON(fiber.Map{"erro": err.Error()})
		}
		if err.Error() == "username ou senha incorretos" {
			return c.Status(401).JSON(fiber.Map{"erro": err.Error()})
		}
		return c.Status(500).JSON(fiber.Map{"erro": "Erro interno"})
	}

	go ah.hub.Broadcast("user_login", "auth", fiber.Map{
		"user_id": res.User.ID, "uuid": res.User.UUID, "username": res.User.Username,
	})

	ah.setRefreshCookie(c, res.RefreshToken, time.Now().Add(30*24*time.Hour))
	return c.Status(200).JSON(res)
}

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
		if err.Error() == "refresh token não informado" {
			return c.Status(400).JSON(fiber.Map{"erro": err.Error()})
		}
		if err.Error() == "sessão inválida ou expirada" || err.Error() == "sessão expirada, faça login novamente" {
			return c.Status(401).JSON(fiber.Map{"erro": err.Error()})
		}
		return c.Status(500).JSON(fiber.Map{"erro": "Erro interno"})
	}

	ah.setRefreshCookie(c, res.RefreshToken, time.Now().Add(30*24*time.Hour))
	return c.JSON(res)
}

func (ah *AuthHandler) Session(c *fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	var tokenStr string
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		tokenStr = authHeader[7:]
	}

	refreshToken := c.Cookies("refresh_token")

	res, err := ah.service.Session(tokenStr, refreshToken)
	if err != nil {
		if err.Error() == "sessão expirada" {
			ah.clearRefreshCookie(c)
			return c.Status(401).JSON(fiber.Map{"authenticated": false, "erro": err.Error()})
		}
		return c.Status(401).JSON(fiber.Map{"authenticated": false, "erro": err.Error()})
	}

	// Update cookie if new token was generated
	if res.RefreshToken != "" {
		ah.setRefreshCookie(c, res.RefreshToken, time.Now().Add(30*24*time.Hour))
	}

	// Dynamic response mapped based on Session return
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

func (ah *AuthHandler) Me(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(int)
	if !ok || userID <= 0 {
		return c.Status(401).JSON(fiber.Map{"erro": "Usuário não autenticado"})
	}

	user, err := ah.service.Me(userID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"erro": err.Error()})
	}

	return c.JSON(fiber.Map{"user": user})
}

func (ah *AuthHandler) Logout(c *fiber.Ctx) error {
	refreshToken := c.Cookies("refresh_token")
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = c.BodyParser(&req)

	tokenToClear := refreshToken
	if req.RefreshToken != "" {
		tokenToClear = req.RefreshToken
	}

	userID, _ := c.Locals("user_id").(int)

	ah.service.Logout(tokenToClear, userID)

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

func (ah *AuthHandler) GetUserByUUID(c *fiber.Ctx) error {
	uuid := c.Params("uuid")

	user, err := ah.service.GetUserByUUID(uuid)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"erro": err.Error()})
	}

	return c.JSON(fiber.Map{
		"id":         user.ID,
		"uuid":       user.UUID,
		"username":   user.Username,
		"created_at": user.CreatedAt,
	})
}

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
