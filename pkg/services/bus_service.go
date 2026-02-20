package services

import (
	"cacc/pkg/cache"
	"cacc/pkg/models"
	"cacc/pkg/repository"
	"fmt"
	"strings"
	"time"
)

type BusService interface {
	ListTrips() ([]models.BusTrip, error)
	CreateTrip(req models.TripCreateRequest) (models.BusTrip, error)
	UpdateTrip(id string, req models.TripUpdateRequest) error
	DeleteTrip(id string) error

	GetSeats(tripID string) ([]models.BusSeat, error)
	Reserve(userID int, tripID string, seatNumber int) (int, error)
	MyReservations(userID int) ([]models.MyReservation, error)
	Cancel(userID int, tripID string, seatNumber int) (int, error)
}

type busService struct {
	repo  repository.BusRepository
	redis *cache.Redis
}

func NewBusService(repo repository.BusRepository, redis *cache.Redis) BusService {
	return &busService{repo: repo, redis: redis}
}

// ── Backoffice / Admin Trips ──

func (s *busService) ListTrips() ([]models.BusTrip, error) {
	cacheKey := "bus:trips:all"
	var cached []models.BusTrip
	if s.redis.Get(cacheKey, &cached) {
		return cached, nil
	}

	trips, err := s.repo.ListTrips()
	if err != nil {
		return nil, err
	}

	s.redis.Set(cacheKey, trips, 5*time.Minute)
	return trips, nil
}

func (s *busService) CreateTrip(req models.TripCreateRequest) (models.BusTrip, error) {
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" {
		return models.BusTrip{}, fmt.Errorf("id da viagem obrigatório")
	}
	if req.TotalSeats <= 0 {
		return models.BusTrip{}, fmt.Errorf("seats precisa ser > 0")
	}

	trip := models.BusTrip{
		ID:            req.ID,
		Name:          req.Name,
		Description:   req.Description,
		DepartureTime: req.DepartureTime,
		TotalSeats:    req.TotalSeats,
	}

	err := s.repo.CreateTrip(trip)
	if err == nil {
		s.redis.Del("bus:trips:all")
	}
	return trip, err
}

func (s *busService) UpdateTrip(id string, req models.TripUpdateRequest) error {
	err := s.repo.UpdateTrip(id, req)
	if err == nil {
		s.redis.Del("bus:trips:all")
		s.redis.Del(fmt.Sprintf("bus:%s:seats", id))
	}
	return err
}

func (s *busService) DeleteTrip(id string) error {
	err := s.repo.DeleteTrip(id)
	if err == nil {
		s.redis.Del("bus:trips:all")
		s.redis.Del(fmt.Sprintf("bus:%s:seats", id))
	}
	return err
}

// ── User Reservations ──

func (s *busService) GetSeats(tripID string) ([]models.BusSeat, error) {
	cacheKey := fmt.Sprintf("bus:%s:seats", tripID)
	var cachedSeats []models.BusSeat
	if s.redis.Get(cacheKey, &cachedSeats) {
		return cachedSeats, nil
	}

	seats, err := s.repo.ListSeats(tripID)
	if err != nil {
		return nil, err
	}

	s.redis.Set(cacheKey, seats, 1*time.Second)
	return seats, nil
}

func (s *busService) Reserve(userID int, tripID string, seatNumber int) (int, error) {
	seat, err := s.repo.ReserveSeat(userID, tripID, seatNumber)
	if err == nil {
		s.redis.Del(fmt.Sprintf("bus:%s:seats", tripID))
	}
	return seat, err
}

func (s *busService) MyReservations(userID int) ([]models.MyReservation, error) {
	return s.repo.MyReservations(userID)
}

func (s *busService) Cancel(userID int, tripID string, seatNumber int) (int, error) {
	seat, err := s.repo.CancelSeat(userID, tripID, seatNumber)
	if err == nil {
		s.redis.Del(fmt.Sprintf("bus:%s:seats", tripID))
	}
	return seat, err
}
