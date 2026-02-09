package models

import "time"

type Sugestao struct {
	ID        int       `json:"id"`
	Texto     string    `json:"texto"`
	CreatedAt time.Time `json:"data_criacao"`
	Author    string    `json:"author"`
	Categoria string    `json:"categoria"`
}
