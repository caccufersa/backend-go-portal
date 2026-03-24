package models

import "time"

// GaleriaItem representa uma imagem publicada na galeria.
type GaleriaItem struct {
	ID         int       `json:"id"`
	UserID     int       `json:"user_id"`
	Author     string    `json:"author"`      // username
	AuthorName string    `json:"author_name"` // display_name ou username
	AvatarURL  string    `json:"avatar_url"`
	ImageURL   string    `json:"image_url"`           // URL Cloudinary
	PublicID   string    `json:"public_id,omitempty"` // ID Cloudinary para deleção
	Caption    string    `json:"caption"`             // legenda opcional
	CreatedAt  time.Time `json:"created_at"`
}
