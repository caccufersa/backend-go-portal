package models

import "time"

type BusSeat struct {
	TripID     string     `json:"trip_id"`
	SeatNumber int        `json:"seat_number"`
	IsReserved bool       `json:"is_reserved"`
	UserID     *int       `json:"user_id,omitempty"`
	ReservedAt *time.Time `json:"reserved_at,omitempty"`
}

type MyReservation struct {
	TripID     string    `json:"trip_id"`
	SeatNumber int       `json:"seat_number"`
	ReservedAt time.Time `json:"reserved_at"`
}

type TripRequest struct {
	TripID     string `json:"trip_id"`
	SeatNumber int    `json:"seat_number"`
}

type BusTrip struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	DepartureTime *time.Time `json:"departure_time,omitempty"`
	TotalSeats    int        `json:"total_seats"`
	IsCompleted   bool       `json:"is_completed"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type TripCreateRequest struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	TotalSeats    int        `json:"total_seats"`
	DepartureTime *time.Time `json:"departure_time,omitempty"`
}

type TripUpdateRequest struct {
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	IsCompleted   *bool      `json:"is_completed,omitempty"`
	DepartureTime *time.Time `json:"departure_time,omitempty"`
}
