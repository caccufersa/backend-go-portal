package repository

import (
	"cacc/pkg/models"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/lib/pq"
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
			       COALESCE(tags, '{}'), created_at, updated_at
			FROM noticias WHERE categoria = $1
			ORDER BY destaque DESC, created_at DESC
			LIMIT $2 OFFSET $3
		`, categoria, limit, offset)
	} else {
		rows, err = r.db.Query(`
			SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
			       COALESCE(tags, '{}'), created_at, updated_at
			FROM noticias
			ORDER BY destaque DESC, created_at DESC
			LIMIT $1 OFFSET $2
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
	var tags pq.StringArray
	err := r.db.QueryRow(`
		SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
		       COALESCE(tags, '{}'), created_at, updated_at
		FROM noticias WHERE id = $1
	`, id).Scan(
		&noticia.ID, &noticia.Titulo, &noticia.Conteudo, &noticia.Resumo, &noticia.Author,
		&noticia.Categoria, &noticia.ImageURL, &noticia.Destaque, &tags, &noticia.CreatedAt, &noticia.UpdatedAt,
	)
	if err != nil {
		return noticia, err
	}
	noticia.Tags = tags
	return noticia, nil
}

func (r *noticiasRepository) Destaques() ([]models.Noticia, error) {
	rows, err := r.db.Query(`
		SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
		       COALESCE(tags, '{}'), created_at, updated_at
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
	var tags pq.StringArray
	err := r.db.QueryRow(`
		INSERT INTO noticias (titulo, conteudo, resumo, author, categoria, image_url, destaque, tags)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
		          COALESCE(tags, '{}'), created_at, updated_at
	`, n.Titulo, n.Conteudo, n.Resumo, n.Author, n.Categoria, n.ImageURL, n.Destaque, pq.Array(n.Tags)).Scan(
		&n.ID, &n.Titulo, &n.Conteudo, &n.Resumo, &n.Author,
		&n.Categoria, &n.ImageURL, &n.Destaque, &tags, &n.CreatedAt, &n.UpdatedAt,
	)
	n.Tags = tags
	return n, err
}

func (r *noticiasRepository) Atualizar(id int, req models.AtualizarNoticiaRequest, conteudoStr string) (models.Noticia, error) {
	sets := []string{}
	args := []interface{}{}
	argIdx := 1

	if req.Titulo != nil {
		sets = append(sets, "titulo = $"+strconv.Itoa(argIdx))
		args = append(args, *req.Titulo)
		argIdx++
	}
	if req.Conteudo != nil && conteudoStr != "" {
		sets = append(sets, "conteudo = $"+strconv.Itoa(argIdx))
		args = append(args, conteudoStr)
		argIdx++
	}
	if req.Resumo != nil {
		sets = append(sets, "resumo = $"+strconv.Itoa(argIdx))
		args = append(args, *req.Resumo)
		argIdx++
	}
	if req.Categoria != nil {
		sets = append(sets, "categoria = $"+strconv.Itoa(argIdx))
		args = append(args, *req.Categoria)
		argIdx++
	}
	if req.ImageURL != nil {
		sets = append(sets, "image_url = $"+strconv.Itoa(argIdx))
		args = append(args, *req.ImageURL)
		argIdx++
	}
	if req.Destaque != nil {
		sets = append(sets, "destaque = $"+strconv.Itoa(argIdx))
		args = append(args, *req.Destaque)
		argIdx++
	}
	if req.Tags != nil {
		sets = append(sets, "tags = $"+strconv.Itoa(argIdx))
		args = append(args, pq.Array(req.Tags))
		argIdx++
	}

	if len(sets) == 0 {
		return models.Noticia{}, fmt.Errorf("nenhum campo para atualizar")
	}

	sets = append(sets, "updated_at = NOW()")
	query := "UPDATE noticias SET " + strings.Join(sets, ", ") + " WHERE id = $" + strconv.Itoa(argIdx)
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
	result, err := r.db.Exec(`DELETE FROM noticias WHERE id = $1`, id)
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
		var tags pq.StringArray
		if err := rows.Scan(
			&noticia.ID, &noticia.Titulo, &noticia.Conteudo, &noticia.Resumo, &noticia.Author,
			&noticia.Categoria, &noticia.ImageURL, &noticia.Destaque, &tags, &noticia.CreatedAt, &noticia.UpdatedAt,
		); err != nil {
			continue
		}
		noticia.Tags = tags
		noticias = append(noticias, noticia)
	}
	return noticias, nil
}
