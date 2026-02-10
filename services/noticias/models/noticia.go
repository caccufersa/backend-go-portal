package models

import "time"

type Noticia struct {
	ID        int       `json:"id"`
	Titulo    string    `json:"titulo"`
	Conteudo  string    `json:"conteudo"`
	Resumo    string    `json:"resumo"`
	Author    string    `json:"author"`
	Categoria string    `json:"categoria"`
	ImageURL  string    `json:"image_url,omitempty"`
	Destaque  bool      `json:"destaque"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CriarNoticiaRequest struct {
	Titulo    string `json:"titulo"`
	Conteudo  string `json:"conteudo"`
	Resumo    string `json:"resumo"`
	Author    string `json:"author"`
	Categoria string `json:"categoria"`
	ImageURL  string `json:"image_url"`
	Destaque  bool   `json:"destaque"`
}

type AtualizarNoticiaRequest struct {
	Titulo    *string `json:"titulo"`
	Conteudo  *string `json:"conteudo"`
	Resumo    *string `json:"resumo"`
	Categoria *string `json:"categoria"`
	ImageURL  *string `json:"image_url"`
	Destaque  *bool   `json:"destaque"`
}
