package handlers

import (
	"strconv"
	"strings"

	"cacc/pkg/hub"
	"cacc/pkg/services"

	"github.com/gofiber/fiber/v2"
)

type SocialHandler struct {
	hub     *hub.Hub
	service services.SocialService
}

func NewSocial(h *hub.Hub, s services.SocialService) *SocialHandler {
	return &SocialHandler{hub: h, service: s}
}

// ──────────────────────────────────────────────
// FEED & THREADS
// ──────────────────────────────────────────────

// GET /social/feed
func (sh *SocialHandler) Feed(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 30)
	offset := c.QueryInt("offset", 0)
	userID, _ := c.Locals("user_id").(int)

	posts, err := sh.service.Feed(limit, offset, userID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao carregar feed"})
	}

	return c.JSON(posts)
}

// GET /social/feed/:id
func (sh *SocialHandler) Thread(c *fiber.Ctx) error {
	postID, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "ID inválido"})
	}

	userID, _ := c.Locals("user_id").(int)

	post, err := sh.service.Thread(postID, userID)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return c.Status(404).JSON(fiber.Map{"erro": "Post não encontrado"})
		}
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao carregar post"})
	}

	return c.JSON(post)
}

// ──────────────────────────────────────────────
// PROFILE
// ──────────────────────────────────────────────

// GET /social/profile/:username?
func (sh *SocialHandler) Profile(c *fiber.Ctx) error {
	username := c.Params("username")
	requestingUserID, _ := c.Locals("user_id").(int)

	profileUserID := 0
	if username == "" {
		if requestingUserID == 0 {
			return c.Status(401).JSON(fiber.Map{"erro": "Não autenticado"})
		}
		profileUserID = requestingUserID
	}

	profile, err := sh.service.Profile(username, profileUserID, requestingUserID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"erro": "Perfil não encontrado"})
	}

	return c.JSON(profile)
}

// PUT /social/profile
func (sh *SocialHandler) UpdateProfile(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(int)
	if !ok || userID == 0 {
		return c.Status(401).JSON(fiber.Map{"erro": "Não autenticado"})
	}

	var req struct {
		DisplayName string `json:"display_name"`
		Bio         string `json:"bio"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}

	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.Bio = strings.TrimSpace(req.Bio)

	if len(req.DisplayName) > 50 {
		return c.Status(400).JSON(fiber.Map{"erro": "Display Name muito longo (máx 50)"})
	}
	if len(req.Bio) > 500 {
		return c.Status(400).JSON(fiber.Map{"erro": "Bio muito longa (máx 500)"})
	}

	if err := sh.service.UpdateProfile(userID, req.DisplayName, req.Bio); err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao atualizar perfil"})
	}

	username, _ := c.Locals("username").(string)
	authorName := username
	if req.DisplayName != "" {
		authorName = req.DisplayName
	}

	go sh.hub.Broadcast("profile_updated", "social", fiber.Map{
		"user_id":      userID,
		"display_name": authorName,
	})

	return c.JSON(fiber.Map{"status": "ok"})
}

// ──────────────────────────────────────────────
// POSTA & INTERACTIONS
// ──────────────────────────────────────────────

// POST /social/feed
func (sh *SocialHandler) CreatePost(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(int)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"erro": "Precisa logar para postar"})
	}
	username, _ := c.Locals("username").(string)

	var req struct {
		Texto string `json:"texto"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}
	texto := strings.TrimSpace(req.Texto)
	if texto == "" {
		return c.Status(400).JSON(fiber.Map{"erro": "Post vazio"})
	}
	if len(texto) > 5000 {
		return c.Status(400).JSON(fiber.Map{"erro": "Post muito longo"})
	}

	post, err := sh.service.CreatePost(texto, username, userID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao salvar post"})
	}

	go sh.hub.Broadcast("new_post", "social", post)
	return c.Status(201).JSON(post)
}

// POST /social/feed/:id/reply
func (sh *SocialHandler) CreateReply(c *fiber.Ctx) error {
	parentID, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "ID inválido"})
	}

	userID, ok := c.Locals("user_id").(int)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"erro": "Não autenticado"})
	}
	username, _ := c.Locals("username").(string)

	var req struct {
		Texto string `json:"texto"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}
	texto := strings.TrimSpace(req.Texto)
	if texto == "" {
		return c.Status(400).JSON(fiber.Map{"erro": "Comentário vazio"})
	}

	reply, err := sh.service.CreateReply(texto, username, userID, parentID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao criar comentário"})
	}

	go sh.hub.Broadcast("new_reply", "social", fiber.Map{
		"reply":     reply,
		"parent_id": parentID,
	})
	return c.Status(201).JSON(reply)
}

func (sh *SocialHandler) LikePost(c *fiber.Ctx) error {
	return sh.handleLikeRequest(c, sh.service.Like)
}

func (sh *SocialHandler) UnlikePost(c *fiber.Ctx) error {
	return sh.handleLikeRequest(c, sh.service.Unlike)
}

func (sh *SocialHandler) handleLikeRequest(c *fiber.Ctx, serviceAction func(int, int) (map[string]interface{}, error)) error {
	postID, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "ID inválido"})
	}

	userID, ok := c.Locals("user_id").(int)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"erro": "Não autenticado"})
	}

	res, err := serviceAction(userID, postID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": err.Error()})
	}

	go sh.hub.Broadcast("post_liked", "social", res)
	return c.JSON(res)
}

func (sh *SocialHandler) DeletePost(c *fiber.Ctx) error {
	postID, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "ID inválido"})
	}

	userID, ok := c.Locals("user_id").(int)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"erro": "Não autenticado"})
	}

	err = sh.service.Delete(userID, postID)
	if err != nil {
		if err.Error() == "post não encontrado ou sem permissão" {
			return c.Status(404).JSON(fiber.Map{"erro": err.Error()})
		}
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao remover"})
	}

	go sh.hub.Broadcast("post_deleted", "social", fiber.Map{
		"post_id": postID,
	})
	return c.JSON(fiber.Map{"status": "deleted", "post_id": postID})
}
