package handlers

import (
	"cacc/pkg/models"
	"cacc/pkg/repository"
	"strconv"

	"github.com/gofiber/fiber/v2"
)

type NotificationHandler struct {
	repo repository.NotificationRepository
}

func NewNotification(repo repository.NotificationRepository) *NotificationHandler {
	return &NotificationHandler{repo: repo}
}

func (h *NotificationHandler) GetNotifications(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(int)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"erro": "Não autenticado"})
	}

	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))

	if limit <= 0 || limit > 50 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	notifs, err := h.repo.GetNotifications(userID, limit, offset)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao carregar notificações"})
	}

	// Mark as read after fetching
	go h.repo.MarkAsRead(userID)

	if notifs == nil {
		notifs = []models.Notification{} // To return empty array instead of null
	}

	return c.JSON(notifs)
}

func (h *NotificationHandler) MarkAsRead(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(int)
	if !ok {
		return c.Status(401).JSON(fiber.Map{"erro": "Não autenticado"})
	}

	err := h.repo.MarkAsRead(userID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao atualizar"})
	}

	return c.JSON(fiber.Map{"status": "ok"})
}
