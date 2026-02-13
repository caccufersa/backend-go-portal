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
	ID           int           `json:"id"`
	Titulo       string        `json:"titulo"`
	Conteudo     string        `json:"conteudo"`                // Armazena JSON do Editor.js como string
	ConteudoObj  *EditorJSData `json:"conteudo_obj,omitempty"`  // Objeto parseado do Editor.js
	ConteudoHTML string        `json:"conteudo_html,omitempty"` // HTML renderizado (opcional, para compatibilidade)
	Resumo       string        `json:"resumo"`
	Author       string        `json:"author"`
	Categoria    string        `json:"categoria"`
	ImageURL     string        `json:"image_url,omitempty"`
	Destaque     bool          `json:"destaque"`
	Tags         []string      `json:"tags,omitempty"` // Tags para categorização adicional
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

type CriarNoticiaRequest struct {
	Titulo       string      `json:"titulo"`
	Conteudo     interface{} `json:"conteudo"`                // Aceita string (texto simples/HTML) ou objeto JSON (Editor.js)
	ConteudoHTML string      `json:"conteudo_html,omitempty"` // HTML renderizado opcional
	Resumo       string      `json:"resumo"`
	Author       string      `json:"author"`
	Categoria    string      `json:"categoria"`
	ImageURL     string      `json:"image_url"`
	Destaque     bool        `json:"destaque"`
	Tags         []string    `json:"tags,omitempty"`
}

type AtualizarNoticiaRequest struct {
	Titulo       *string     `json:"titulo"`
	Conteudo     interface{} `json:"conteudo"` // Aceita string ou objeto JSON
	ConteudoHTML *string     `json:"conteudo_html,omitempty"`
	Resumo       *string     `json:"resumo"`
	Categoria    *string     `json:"categoria"`
	ImageURL     *string     `json:"image_url"`
	Destaque     *bool       `json:"destaque"`
	Tags         []string    `json:"tags,omitempty"`
}

func ParseConteudo(conteudo interface{}) (string, error) {
	switch v := conteudo.(type) {
	case string:
		return v, nil
	case map[string]interface{}, []interface{}:
		bytes, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(bytes), nil
	default:
		bytes, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(bytes), nil
	}
}
