package handlers

import (
	"cacc/pkg/cache"
	"cacc/pkg/models"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
)

type BusHandler struct {
	db    *sql.DB
	redis *cache.Redis

	// SQL Statements
	stmtListSeats   *sql.Stmt
	stmtReserveSeat *sql.Stmt
	stmtMySeats     *sql.Stmt
	stmtCancelSeat  *sql.Stmt
}

func NewBus(db *sql.DB, r *cache.Redis) *BusHandler {
	h := &BusHandler{db: db, redis: r}
	h.prepareStatements()
	return h
}

func (h *BusHandler) prepareStatements() {
	var err error

	// Listar todos os assentos (ordenados)
	h.stmtListSeats, err = h.db.Prepare(`
		SELECT seat_number, user_id, reserved_at 
		FROM bus_seats 
		WHERE bus_id = $1 
		ORDER BY seat_number ASC
	`)
	if err != nil {
		log.Fatalf("[BUS] FATAL: prepare list: %v", err)
	}

	// Reserve only if user_id is NULL to ensure atomicity
	h.stmtReserveSeat, err = h.db.Prepare(`
		UPDATE bus_seats 
		SET user_id = $1, reserved_at = NOW() 
		WHERE bus_id = $2 AND seat_number = $3 AND user_id IS NULL
		RETURNING seat_number
	`)
	if err != nil {
		log.Fatalf("[BUS] FATAL: prepare reserve: %v", err)
	}

	h.stmtMySeats, err = h.db.Prepare(`
		SELECT bus_id, seat_number, reserved_at
		FROM bus_seats
		WHERE user_id = $1
		ORDER BY reserved_at DESC
	`)
	if err != nil {
		log.Fatalf("[BUS] FATAL: prepare my seats: %v", err)
	}

	h.stmtCancelSeat, err = h.db.Prepare(`
		UPDATE bus_seats
		SET user_id = NULL, reserved_at = NULL
		WHERE bus_id = $1 AND seat_number = $2 AND user_id = $3
		RETURNING seat_number
	`)
	if err != nil {
		log.Fatalf("[BUS] FATAL: prepare cancel: %v", err)
	}
}

func (h *BusHandler) GetSeats(c *fiber.Ctx) error {
	tripID := c.Params("id")
	if tripID == "" {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid Trip ID"})
	}

	cacheKey := fmt.Sprintf("bus:%s:seats", tripID)
	var cachedSeats []models.BusSeat
	if h.redis.Get(cacheKey, &cachedSeats) {
		return c.JSON(cachedSeats)
	}

	rows, err := h.stmtListSeats.Query(tripID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "DB Error"})
	}
	defer rows.Close()

	seats := []models.BusSeat{}
	for rows.Next() {
		var s models.BusSeat
		var uid sql.NullInt64
		var rat sql.NullTime

		if err := rows.Scan(&s.SeatNumber, &uid, &rat); err != nil {
			continue
		}

		s.TripID = tripID
		if uid.Valid {
			s.IsReserved = true
			id := int(uid.Int64)
			s.UserID = &id
			if rat.Valid {
				t := rat.Time
				s.ReservedAt = &t
			}
		}
		seats = append(seats, s)
	}

	h.redis.Set(cacheKey, seats, 1*time.Second) // 1s micro-cache

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

	// Atomic Update
	var reservedSeat int
	err := h.stmtReserveSeat.QueryRow(userID, req.TripID, req.SeatNumber).Scan(&reservedSeat)

	if err == sql.ErrNoRows {
		return c.Status(409).JSON(fiber.Map{"error": "Seat already reserved"})
	}
	if err != nil {
		log.Printf("[BUS] Error reserving: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "Reservation Failed"})
	}

	h.redis.Del(fmt.Sprintf("bus:%s:seats", req.TripID))

	return c.JSON(fiber.Map{"status": "reserved", "seat": reservedSeat})
}

func (h *BusHandler) MyReservations(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(int)
	if !ok || userID <= 0 {
		return c.Status(401).JSON(fiber.Map{"error": "Unauthorized"})
	}

	rows, err := h.stmtMySeats.Query(userID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "DB Error"})
	}
	defer rows.Close()

	list := []models.MyReservation{}
	for rows.Next() {
		var s models.MyReservation
		rows.Scan(&s.TripID, &s.SeatNumber, &s.ReservedAt)
		list = append(list, s)
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

	var seat int
	err := h.stmtCancelSeat.QueryRow(req.TripID, req.SeatNumber, userID).Scan(&seat)
	if err == sql.ErrNoRows {
		return c.Status(404).JSON(fiber.Map{"error": "Reservation not found or not yours"})
	}

	h.redis.Del(fmt.Sprintf("bus:%s:seats", req.TripID))

	return c.JSON(fiber.Map{"status": "cancelled", "seat": seat})
}
