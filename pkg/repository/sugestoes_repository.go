package repository

import (
	"cacc/pkg/models"
	"database/sql"
)

type SugestoesRepository interface {
	Listar() ([]models.Sugestao, error)
	Criar(texto, author, categoria string) (models.Sugestao, error)
}

type sugestoesRepository struct {
	db *sql.DB
}

func NewSugestoesRepository(db *sql.DB) SugestoesRepository {
	return &sugestoesRepository{db: db}
}

func (r *sugestoesRepository) Listar() ([]models.Sugestao, error) {
	rows, err := r.db.Query(`
		SELECT id, texto, data_criacao, COALESCE(author, 'Anônimo'), COALESCE(categoria, 'Geral')
		FROM sugestoes ORDER BY id DESC LIMIT 200
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lista []models.Sugestao
	for rows.Next() {
		var s models.Sugestao
		if err := rows.Scan(&s.ID, &s.Texto, &s.CreatedAt, &s.Author, &s.Categoria); err != nil {
			continue
		}
		lista = append(lista, s)
	}
	return lista, nil
}

func (r *sugestoesRepository) Criar(texto, author, categoria string) (models.Sugestao, error) {
	var s models.Sugestao
	err := r.db.QueryRow(`
		INSERT INTO sugestoes (texto, author, categoria)
		VALUES ($1, $2, $3)
		RETURNING id, data_criacao
	`, texto, author, categoria).Scan(&s.ID, &s.CreatedAt)
	if err != nil {
		return s, err
	}

	s.Texto = texto
	s.Author = author
	s.Categoria = categoria
	return s, nil
}
