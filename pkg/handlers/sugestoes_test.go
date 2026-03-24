package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cacc/pkg/models"

	"github.com/gofiber/fiber/v2"
)

type sugestoesServiceMock struct {
	created models.Sugestao
	err     error

	lastTexto     string
	lastAuthor    string
	lastCategoria string
}

func (m *sugestoesServiceMock) Listar() ([]models.Sugestao, error) {
	return nil, nil
}

func (m *sugestoesServiceMock) Criar(texto, author, categoria string) (models.Sugestao, error) {
	m.lastTexto = texto
	m.lastAuthor = author
	m.lastCategoria = categoria
	return m.created, m.err
}

func (m *sugestoesServiceMock) Deletar(id int) error {
	return nil
}

func (m *sugestoesServiceMock) Atualizar(id int, texto, categoria string) error {
	return nil
}

func TestSugestoesPostSemLogin(t *testing.T) {
	t.Parallel()

	mock := &sugestoesServiceMock{
		created: models.Sugestao{
			ID:        1,
			Texto:     "Sugestão para o portal",
			Author:    "Anônimo",
			Categoria: "Melhoria",
			CreatedAt: time.Now(),
		},
	}

	h := NewSugestoes(mock)
	app := fiber.New()
	app.Post("/sugestoes", h.Criar)

	body := map[string]any{
		"texto":     "Sugestão para o portal",
		"categoria": "Melhoria",
		"anonimo":   true,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("falha ao serializar payload: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/sugestoes", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("falha ao executar request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status esperado %d, obtido %d", http.StatusCreated, resp.StatusCode)
	}

	if mock.lastAuthor != "Anônimo" {
		t.Fatalf("author esperado 'Anônimo', obtido '%s'", mock.lastAuthor)
	}
	if mock.lastTexto != "Sugestão para o portal" {
		t.Fatalf("texto esperado não foi enviado ao service")
	}
	if mock.lastCategoria != "Melhoria" {
		t.Fatalf("categoria esperada 'Melhoria', obtida '%s'", mock.lastCategoria)
	}

	var got models.Sugestao
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("falha ao parsear resposta JSON: %v", err)
	}

	if got.Author != "Anônimo" {
		t.Fatalf("resposta deveria conter author 'Anônimo', obtido '%s'", got.Author)
	}
}
