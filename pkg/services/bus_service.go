package services

import (
	"cacc/pkg/cache"
	"cacc/pkg/models"
	"cacc/pkg/repository"
	"fmt"
	"time"
)

type BusService interface {
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
