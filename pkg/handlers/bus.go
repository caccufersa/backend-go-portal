package handlers

import (
	"cacc/pkg/models"
	"cacc/pkg/services"
	"database/sql"
	"log"

	"github.com/gofiber/fiber/v2"
)

type BusHandler struct {
	service services.BusService
}

func NewBus(service services.BusService) *BusHandler {
	return &BusHandler{service: service}
}

func (h *BusHandler) GetSeats(c *fiber.Ctx) error {
	tripID := c.Params("id")
	if tripID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid Trip ID"})
	}

	seats, err := h.service.GetSeats(tripID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "DB Error"})
	}

	return c.JSON(seats)
}

func (h *BusHandler) Reserve(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(int)
	if !ok || userID <= 0 {
		return c.Status(401).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req struct {
		TripID     string `json:"trip_id"`
		SeatNumber int    `json:"seat_number"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid Body"})
	}

	if req.TripID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Missing Trip ID"})
	}

	reservedSeat, err := h.service.Reserve(userID, req.TripID, req.SeatNumber)
	if err == sql.ErrNoRows {
		return c.Status(409).JSON(fiber.Map{"error": "Seat already reserved"})
	}
	if err != nil {
		log.Printf("[BUS] Error reserving: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "Reservation Failed"})
	}

	return c.JSON(fiber.Map{"status": "reserved", "seat": reservedSeat})
}

func (h *BusHandler) MyReservations(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(int)
	if !ok || userID <= 0 {
		return c.Status(401).JSON(fiber.Map{"error": "Unauthorized"})
	}

	list, err := h.service.MyReservations(userID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "DB Error"})
	}

	return c.JSON(list)
}

func (h *BusHandler) Cancel(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(int)
	if !ok || userID <= 0 {
		return c.Status(401).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req models.TripRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid Body"})
	}

	seat, err := h.service.Cancel(userID, req.TripID, req.SeatNumber)
	if err == sql.ErrNoRows {
		return c.Status(404).JSON(fiber.Map{"error": "Reservation not found or not yours"})
	}
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "Cancellation failed"})
	}

	return c.JSON(fiber.Map{"status": "cancelled", "seat": seat})
}
