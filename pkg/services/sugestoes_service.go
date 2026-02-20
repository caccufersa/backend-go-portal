package services

import (
	"cacc/pkg/cache"
	"cacc/pkg/models"
	"cacc/pkg/repository"
	"time"
)

type SugestoesService interface {
	Listar() ([]models.Sugestao, error)
	Criar(texto, author, categoria string) (models.Sugestao, error)
}

type sugestoesService struct {
	repo  repository.SugestoesRepository
	redis *cache.Redis
}

func NewSugestoesService(repo repository.SugestoesRepository, redis *cache.Redis) SugestoesService {
	return &sugestoesService{repo: repo, redis: redis}
}

func (s *sugestoesService) Listar() ([]models.Sugestao, error) {
	var cached []models.Sugestao
	if s.redis.Get("sugestoes:all", &cached) {
		return cached, nil
	}

	lista, err := s.repo.Listar()
	if err != nil {
		return nil, err
	}

	s.redis.Set("sugestoes:all", lista, 30*time.Second)
	return lista, nil
}

func (s *sugestoesService) Criar(texto, author, categoria string) (models.Sugestao, error) {
	sugestao, err := s.repo.Criar(texto, author, categoria)
	if err == nil {
		s.redis.Del("sugestoes:all")
	}
	return sugestao, err
}
