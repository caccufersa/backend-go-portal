package repository

import (
	"cacc/pkg/models"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type NoticiasRepository interface {
	Listar(categoria string, limit, offset int) ([]models.Noticia, error)
	BuscarPorID(id int) (models.Noticia, error)
	Destaques() ([]models.Noticia, error)
	Criar(n models.Noticia) (models.Noticia, error)
	Atualizar(id int, req models.AtualizarNoticiaRequest, conteudoStr string) (models.Noticia, error)
	Deletar(id int) (bool, error)
}

type noticiasRepository struct {
	db *sql.DB
}

func NewNoticiasRepository(db *sql.DB) NoticiasRepository {
	return &noticiasRepository{db: db}
}

func (r *noticiasRepository) Listar(categoria string, limit, offset int) ([]models.Noticia, error) {
	var rows *sql.Rows
	var err error

	if categoria != "" {
		rows, err = r.db.Query(`
			SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
			       COALESCE(tags, '[]'), created_at, updated_at
			FROM noticias WHERE categoria = ?
			ORDER BY destaque DESC, created_at DESC
			LIMIT ? OFFSET ?
		`, categoria, limit, offset)
	} else {
		rows, err = r.db.Query(`
			SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
			       COALESCE(tags, '[]'), created_at, updated_at
			FROM noticias
			ORDER BY destaque DESC, created_at DESC
			LIMIT ? OFFSET ?
		`, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanNoticias(rows)
}

func (r *noticiasRepository) BuscarPorID(id int) (models.Noticia, error) {
	var noticia models.Noticia
	var tagsJSON string

	err := r.db.QueryRow(`
		SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
		       COALESCE(tags, '[]'), created_at, updated_at
		FROM noticias WHERE id = ?
	`, id).Scan(
		&noticia.ID, &noticia.Titulo, &noticia.Conteudo, &noticia.Resumo, &noticia.Author,
		&noticia.Categoria, &noticia.ImageURL, &noticia.Destaque, &tagsJSON, &noticia.CreatedAt, &noticia.UpdatedAt,
	)
	if err != nil {
		return noticia, err
	}

	if tagsJSON != "" && tagsJSON != "[]" {
		var tags []string
		if err := json.Unmarshal([]byte(tagsJSON), &tags); err == nil {
			noticia.Tags = tags
		}
	}

	return noticia, nil
}

func (r *noticiasRepository) Destaques() ([]models.Noticia, error) {
	rows, err := r.db.Query(`
		SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
		       COALESCE(tags, '[]'), created_at, updated_at
		FROM noticias WHERE destaque = true
		ORDER BY created_at DESC LIMIT 10
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanNoticias(rows)
}

func (r *noticiasRepository) Criar(n models.Noticia) (models.Noticia, error) {
	tagsJSON, _ := json.Marshal(n.Tags)

	_, err := r.db.Exec(`
		INSERT INTO noticias (titulo, conteudo, resumo, author, categoria, image_url, destaque, tags)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, n.Titulo, n.Conteudo, n.Resumo, n.Author, n.Categoria, n.ImageURL, n.Destaque, string(tagsJSON))
	if err != nil {
		return n, err
	}

	return n, nil
}

func (r *noticiasRepository) Atualizar(id int, req models.AtualizarNoticiaRequest, conteudoStr string) (models.Noticia, error) {
	sets := []string{}
	args := []interface{}{}

	if req.Titulo != nil {
		sets = append(sets, "titulo = ?")
		args = append(args, *req.Titulo)
	}
	if req.Conteudo != nil && conteudoStr != "" {
		sets = append(sets, "conteudo = ?")
		args = append(args, conteudoStr)
	}
	if req.Resumo != nil {
		sets = append(sets, "resumo = ?")
		args = append(args, *req.Resumo)
	}
	if req.Categoria != nil {
		sets = append(sets, "categoria = ?")
		args = append(args, *req.Categoria)
	}
	if req.ImageURL != nil {
		sets = append(sets, "image_url = ?")
		args = append(args, *req.ImageURL)
	}
	if req.Destaque != nil {
		sets = append(sets, "destaque = ?")
		args = append(args, *req.Destaque)
	}
	if req.Tags != nil {
		tagsJSON, _ := json.Marshal(req.Tags)
		sets = append(sets, "tags = ?")
		args = append(args, string(tagsJSON))
	}

	if len(sets) == 0 {
		return models.Noticia{}, fmt.Errorf("nenhum campo para atualizar")
	}

	sets = append(sets, "updated_at = NOW()")
	query := "UPDATE noticias SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	args = append(args, id)

	result, err := r.db.Exec(query, args...)
	if err != nil {
		return models.Noticia{}, err
	}

	rowsAff, _ := result.RowsAffected()
	if rowsAff == 0 {
		return models.Noticia{}, sql.ErrNoRows
	}

	return r.BuscarPorID(id)
}

func (r *noticiasRepository) Deletar(id int) (bool, error) {
	result, err := r.db.Exec(`DELETE FROM noticias WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	rowsAff, _ := result.RowsAffected()
	return rowsAff > 0, nil
}

func scanNoticias(rows *sql.Rows) ([]models.Noticia, error) {
	var noticias []models.Noticia
	for rows.Next() {
		var noticia models.Noticia
		var tagsJSON string

		if err := rows.Scan(
			&noticia.ID, &noticia.Titulo, &noticia.Conteudo, &noticia.Resumo, &noticia.Author,
			&noticia.Categoria, &noticia.ImageURL, &noticia.Destaque, &tagsJSON, &noticia.CreatedAt, &noticia.UpdatedAt,
		); err != nil {
			continue
		}

		if tagsJSON != "" && tagsJSON != "[]" {
			var tags []string
			if err := json.Unmarshal([]byte(tagsJSON), &tags); err == nil {
				noticia.Tags = tags
			}
		}

		noticias = append(noticias, noticia)
	}
	return noticias, nil
}
