package models

import "time"

type Sugestao struct {
	ID        int       `json:"id"`
	Texto     string    `json:"texto"`
	CreatedAt time.Time `json:"created_at"`
	Author    string    `json:"author"`
	Categoria string    `json:"categoria"`
}
