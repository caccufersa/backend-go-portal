package repository

import (
	"cacc/pkg/models"
	"database/sql"
)

type BusRepository interface {
	ListTrips() ([]models.BusTrip, error)
	CreateTrip(trip models.BusTrip) error
	UpdateTrip(id string, trip models.TripUpdateRequest) error
	DeleteTrip(id string) error

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

// ---- Trips Management ----

func (r *busRepository) ListTrips() ([]models.BusTrip, error) {
	rows, err := r.db.Query(`
		SELECT id, name, description, departure_time, total_seats, is_completed, created_at, updated_at
		FROM bus_trips
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trips []models.BusTrip
	for rows.Next() {
		var t models.BusTrip
		var deptTime sql.NullTime
		var desc sql.NullString

		if err := rows.Scan(&t.ID, &t.Name, &desc, &deptTime, &t.TotalSeats, &t.IsCompleted, &t.CreatedAt, &t.UpdatedAt); err != nil {
			continue
		}

		if desc.Valid {
			t.Description = desc.String
		}
		if deptTime.Valid {
			tm := deptTime.Time
			t.DepartureTime = &tm
		}
		trips = append(trips, t)
	}
	return trips, nil
}

func (r *busRepository) CreateTrip(trip models.BusTrip) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		INSERT INTO bus_trips (id, name, description, departure_time, total_seats)
		VALUES ($1, $2, $3, $4, $5)
	`, trip.ID, trip.Name, trip.Description, trip.DepartureTime, trip.TotalSeats)

	if err != nil {
		tx.Rollback()
		return err
	}

	// Create seats automatically
	_, err = tx.Exec(`
		INSERT INTO bus_seats (trip_id, seat_number)
		SELECT $1, generate_series(1, $2)
	`, trip.ID, trip.TotalSeats)

	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (r *busRepository) UpdateTrip(id string, req models.TripUpdateRequest) error {
	// Base query updates unconditionally:
	query := `
		UPDATE bus_trips
		SET updated_at = NOW()
	`
	args := []interface{}{}
	argId := 1

	if req.Name != "" {
		query += `, name = $` + string(rune(argId+'0'))
		args = append(args, req.Name)
		argId++
	}
	if req.Description != "" {
		query += `, description = $` + string(rune(argId+'0'))
		args = append(args, req.Description)
		argId++
	}
	if req.DepartureTime != nil {
		query += `, departure_time = $` + string(rune(argId+'0'))
		args = append(args, *req.DepartureTime)
		argId++
	}
	if req.IsCompleted != nil {
		query += `, is_completed = $` + string(rune(argId+'0'))
		args = append(args, *req.IsCompleted)
		argId++
	}

	query += ` WHERE id = $` + string(rune(argId+'0'))
	args = append(args, id)

	_, err := r.db.Exec(query, args...)
	return err
}

func (r *busRepository) DeleteTrip(id string) error {
	// Cascate constraints handle bus_seats mappings natively
	_, err := r.db.Exec(`DELETE FROM bus_trips WHERE id = $1`, id)
	return err
}

// ---- Standard Reservations Management ----

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
		  AND EXISTS (SELECT 1 FROM bus_trips WHERE id = $2 AND is_completed = false)
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
