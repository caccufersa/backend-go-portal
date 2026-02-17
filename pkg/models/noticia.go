package models

import (
	"encoding/json"
	"time"
)

type EditorJSData struct {
	Time    int64                    `json:"time"`
	Blocks  []map[string]interface{} `json:"blocks"`
	Version string                   `json:"version"`
}

type Noticia struct {
	ID          int           `json:"id"`
	Titulo      string        `json:"titulo"`
	Conteudo    string        `json:"conteudo"`
	ConteudoObj *EditorJSData `json:"conteudo_obj,omitempty"`
	Resumo      string        `json:"resumo"`
	Author      string        `json:"author"`
	Categoria   string        `json:"categoria"`
	ImageURL    string        `json:"image_url,omitempty"`
	Destaque    bool          `json:"destaque"`
	Tags        []string      `json:"tags,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type CriarNoticiaRequest struct {
	Titulo    string      `json:"titulo"`
	Conteudo  interface{} `json:"conteudo"`
	Resumo    string      `json:"resumo"`
	Author    string      `json:"author"`
	Categoria string      `json:"categoria"`
	ImageURL  string      `json:"image_url"`
	Destaque  bool        `json:"destaque"`
	Tags      []string    `json:"tags,omitempty"`
}

type AtualizarNoticiaRequest struct {
	Titulo    *string     `json:"titulo"`
	Conteudo  interface{} `json:"conteudo"`
	Resumo    *string     `json:"resumo"`
	Categoria *string     `json:"categoria"`
	ImageURL  *string     `json:"image_url"`
	Destaque  *bool       `json:"destaque"`
	Tags      []string    `json:"tags,omitempty"`
}

func ParseConteudo(conteudo interface{}) (string, error) {
	switch v := conteudo.(type) {
	case string:
		return v, nil
	default:
		bytes, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(bytes), nil
	}
}
