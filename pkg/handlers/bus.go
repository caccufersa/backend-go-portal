package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"time"

	"cacc/pkg/cache"

	"github.com/gofiber/fiber/v2"
)

type BusHandler struct {
	db    *sql.DB
	redis *cache.Redis

	// Prepared statements for massive speed
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

	// Reserva atômica: Só reserva se user_id for NULL (livre)
	// Isso garante concorrência sem locks complexos: o banco serializa no nível da linha.
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

// Models
type Seat struct {
	Number     int        `json:"seat_number"`
	IsReserved bool       `json:"is_reserved"`
	UserID     *int       `json:"user_id,omitempty"` // Opcional, talvez só retornar se for o próprio usuário
	ReservedAt *time.Time `json:"reserved_at,omitempty"`
}

// ──────────────────────────────────────────────
// ENDPOINTS
// ──────────────────────────────────────────────

// GetSeats - Ultra fast cached list
func (h *BusHandler) GetSeats(c *fiber.Ctx) error {
	busID, _ := strconv.Atoi(c.Params("id"))
	if busID <= 0 {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid Bus ID"})
	}

	// 1. Try Cache First (Micro-caching 1s for high concurrency reads)
	cacheKey := fmt.Sprintf("bus:%d:seats", busID)
	var cachedSeats []Seat
	if h.redis.Get(cacheKey, &cachedSeats) {
		return c.JSON(cachedSeats)
	}

	// 2. DB Hit
	rows, err := h.stmtListSeats.Query(busID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "DB Error"})
	}
	defer rows.Close()

	seats := []Seat{}
	for rows.Next() {
		var s Seat
		var uid sql.NullInt64
		var rat sql.NullTime

		if err := rows.Scan(&s.Number, &uid, &rat); err != nil {
			continue
		}

		if uid.Valid {
			s.IsReserved = true
			id := int(uid.Int64)
			s.UserID = &id // In production, maybe hide this from other users
			if rat.Valid {
				t := rat.Time
				s.ReservedAt = &t
			}
		}
		seats = append(seats, s)
	}

	// 3. Set Cache (Short TTL to reflect updates quickly but protect DB from storms)
	h.redis.Set(cacheKey, seats, 1*time.Second)

	return c.JSON(seats)
}

// Reserve - Atomic & Thread-safe
func (h *BusHandler) Reserve(c *fiber.Ctx) error {
	// Pega UserID do token JWT injetado pelo middleware
	userID, ok := c.Locals("user_id").(int)
	if !ok || userID <= 0 {
		return c.Status(401).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req struct {
		BusID      int `json:"bus_id"`
		SeatNumber int `json:"seat_number"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid Body"})
	}

	// Atomic Update
	var reservedSeat int
	err := h.stmtReserveSeat.QueryRow(userID, req.BusID, req.SeatNumber).Scan(&reservedSeat)

	if err == sql.ErrNoRows {
		// Se não retornou linha, é porque o WHERE falhou (user_id IS NOT NULL)
		// Ou seja, já estava reservado. Race condition handled by DB.
		return c.Status(409).JSON(fiber.Map{"error": "Seat already reserved"})
	}
	if err != nil {
		log.Printf("[BUS] Error reserving: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "Reservation Failed"})
	}

	// Invalidate Cache Immediately
	h.redis.Del(fmt.Sprintf("bus:%d:seats", req.BusID))

	// Broadcast update via WebSocket (optional, but requested "speed/realtime")
	// h.hub.Broadcast("seat_reserved", "bus", map[string]int{"bus": req.BusID, "seat": reservedSeat})

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

	type MySeat struct {
		BusID      int       `json:"bus_id"`
		SeatNumber int       `json:"seat_number"`
		ReservedAt time.Time `json:"reserved_at"`
	}

	list := []MySeat{}
	for rows.Next() {
		var s MySeat
		rows.Scan(&s.BusID, &s.SeatNumber, &s.ReservedAt)
		list = append(list, s)
	}

	return c.JSON(list)
}

func (h *BusHandler) Cancel(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(int)
	if !ok || userID <= 0 {
		return c.Status(401).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req struct {
		BusID      int `json:"bus_id"`
		SeatNumber int `json:"seat_number"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "Invalid Body"})
	}

	var seat int
	err := h.stmtCancelSeat.QueryRow(req.BusID, req.SeatNumber, userID).Scan(&seat)
	if err == sql.ErrNoRows {
		return c.Status(404).JSON(fiber.Map{"error": "Reservation not found or not yours"})
	}

	// Invalidate Cache
	h.redis.Del(fmt.Sprintf("bus:%d:seats", req.BusID))

	return c.JSON(fiber.Map{"status": "cancelled", "seat": seat})
}
