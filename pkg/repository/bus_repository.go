package repository

import (
	"cacc/pkg/models"
	"database/sql"
)

type BusRepository interface {
	ListSeats(tripID string) ([]models.BusSeat, error)
	ReserveSeat(userID int, tripID string, seatNumber int) (int, error)
	MyReservations(userID int) ([]models.MyReservation, error)
	CancelSeat(userID int, tripID string, seatNumber int) (int, error)
}

type busRepository struct {
	db *sql.DB
}

func NewBusRepository(db *sql.DB) BusRepository {
	return &busRepository{db: db}
}

func (r *busRepository) ListSeats(tripID string) ([]models.BusSeat, error) {
	rows, err := r.db.Query(`
		SELECT seat_number, user_id, reserved_at 
		FROM bus_seats 
		WHERE trip_id = $1 
		ORDER BY seat_number ASC
	`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var seats []models.BusSeat
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
	return seats, nil
}

func (r *busRepository) ReserveSeat(userID int, tripID string, seatNumber int) (int, error) {
	var reservedSeat int
	err := r.db.QueryRow(`
		UPDATE bus_seats 
		SET user_id = $1, reserved_at = NOW() 
		WHERE trip_id = $2 AND seat_number = $3 AND user_id IS NULL
		RETURNING seat_number
	`, userID, tripID, seatNumber).Scan(&reservedSeat)
	return reservedSeat, err
}

func (r *busRepository) MyReservations(userID int) ([]models.MyReservation, error) {
	rows, err := r.db.Query(`
		SELECT trip_id, seat_number, reserved_at
		FROM bus_seats
		WHERE user_id = $1
		ORDER BY reserved_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []models.MyReservation
	for rows.Next() {
		var s models.MyReservation
		if err := rows.Scan(&s.TripID, &s.SeatNumber, &s.ReservedAt); err == nil {
			list = append(list, s)
		}
	}
	return list, nil
}

func (r *busRepository) CancelSeat(userID int, tripID string, seatNumber int) (int, error) {
	var seat int
	err := r.db.QueryRow(`
		UPDATE bus_seats
		SET user_id = NULL, reserved_at = NULL
		WHERE trip_id = $1 AND seat_number = $2 AND user_id = $3
		RETURNING seat_number
	`, tripID, seatNumber, userID).Scan(&seat)
	return seat, err
}
