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
