package repository

import (
	"cacc/pkg/models"
	"database/sql"
	"time"
)

type GaleriaRepository interface {
	List(limit, offset int) ([]models.GaleriaItem, error)
	Create(userID int, author, authorName, avatarURL, imageURL, publicID, caption string) (models.GaleriaItem, error)
	Delete(id, userID int) error
	GetByID(id int) (models.GaleriaItem, error)
}

type galeriaRepository struct {
	db *sql.DB
}

func NewGaleriaRepository(db *sql.DB) GaleriaRepository {
	return &galeriaRepository{db: db}
}

func (r *galeriaRepository) List(limit, offset int) ([]models.GaleriaItem, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := r.db.Query(`
		SELECT g.id, g.user_id, g.author, g.author_name, g.avatar_url,
		       g.image_url, g.public_id, g.caption, g.created_at
		FROM galeria g
		ORDER BY g.created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.GaleriaItem
	for rows.Next() {
		var it models.GaleriaItem
		var publicID, caption sql.NullString
		if err := rows.Scan(
			&it.ID, &it.UserID, &it.Author, &it.AuthorName, &it.AvatarURL,
			&it.ImageURL, &publicID, &caption, &it.CreatedAt,
		); err != nil {
			return nil, err
		}
		it.PublicID = publicID.String
		it.Caption = caption.String
		items = append(items, it)
	}
	if items == nil {
		items = []models.GaleriaItem{}
	}
	return items, rows.Err()
}

func (r *galeriaRepository) Create(userID int, author, authorName, avatarURL, imageURL, publicID, caption string) (models.GaleriaItem, error) {
	var item models.GaleriaItem
	var pub, cap sql.NullString
	err := r.db.QueryRow(`
		INSERT INTO galeria (user_id, author, author_name, avatar_url, image_url, public_id, caption, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, user_id, author, author_name, avatar_url, image_url, public_id, caption, created_at
	`, userID, author, authorName, avatarURL, imageURL, publicID, caption, time.Now()).
		Scan(&item.ID, &item.UserID, &item.Author, &item.AuthorName, &item.AvatarURL,
			&item.ImageURL, &pub, &cap, &item.CreatedAt)
	item.PublicID = pub.String
	item.Caption = cap.String
	return item, err
}

func (r *galeriaRepository) Delete(id, userID int) error {
	res, err := r.db.Exec(`DELETE FROM galeria WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *galeriaRepository) GetByID(id int) (models.GaleriaItem, error) {
	var item models.GaleriaItem
	var pub, cap sql.NullString
	err := r.db.QueryRow(`
		SELECT id, user_id, author, author_name, avatar_url, image_url, public_id, caption, created_at
		FROM galeria WHERE id = $1
	`, id).Scan(&item.ID, &item.UserID, &item.Author, &item.AuthorName, &item.AvatarURL,
		&item.ImageURL, &pub, &cap, &item.CreatedAt)
	item.PublicID = pub.String
	item.Caption = cap.String
	return item, err
}
