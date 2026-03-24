package repository

import (
	"cacc/pkg/models"
	"database/sql"
	"fmt"
)

type SugestoesRepository interface {
	Listar() ([]models.Sugestao, error)
	Criar(texto, author, categoria string) (models.Sugestao, error)
	Deletar(id int) error
	Atualizar(id int, texto, categoria string) error
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
	res, err := r.db.Exec(`
		INSERT INTO sugestoes (texto, author, categoria)
		VALUES (?, ?, ?)
	`, texto, author, categoria)
	if err != nil {
		return s, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return s, fmt.Errorf("falha ao obter id da sugestão criada: %w", err)
	}

	err = r.db.QueryRow(`
		SELECT data_criacao FROM sugestoes WHERE id = ?
	`, id).Scan(&s.CreatedAt)
	if err != nil {
		return s, err
	}

	s.ID = int(id)
	s.Texto = texto
	s.Author = author
	s.Categoria = categoria
	return s, nil
}
func (r *sugestoesRepository) Deletar(id int) error {
	_, err := r.db.Exec(`DELETE FROM sugestoes WHERE id = ?`, id)
	return err
}

func (r *sugestoesRepository) Atualizar(id int, texto, categoria string) error {
	_, err := r.db.Exec(`UPDATE sugestoes SET texto = ?, categoria = ? WHERE id = ?`, texto, categoria, id)
	return err
}
