package handlers

import (
	"database/sql"
	"log"

	"cacc/services/sugestoes/models"

	"github.com/gofiber/fiber/v2"
)

type SugestaoHandler struct {
	DB *sql.DB

	OnCreate func(s models.Sugestao)
}

func New(db *sql.DB) *SugestaoHandler {
	return &SugestaoHandler{DB: db}
}

func (h *SugestaoHandler) Listar(c *fiber.Ctx) error {
	query := `
		SELECT id, texto, data_criacao, COALESCE(author, 'Anônimo'), COALESCE(categoria, 'Geral')
		FROM sugestoes 
		ORDER BY id DESC
	`
	rows, err := h.DB.Query(query)
	if err != nil {
		log.Println("Erro Query:", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"erro": "Erro no banco"})
	}
	defer rows.Close()

	lista := []models.Sugestao{}
	for rows.Next() {
		var s models.Sugestao
		if err := rows.Scan(&s.ID, &s.Texto, &s.CreatedAt, &s.Author, &s.Categoria); err != nil {
			log.Println("Erro Scan:", err)
			continue
		}
		lista = append(lista, s)
	}

	return c.JSON(lista)
}

func (h *SugestaoHandler) Criar(c *fiber.Ctx) error {
	var s models.Sugestao
	if err := c.BodyParser(&s); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"erro": "JSON inválido"})
	}

	if s.Author == "" {
		s.Author = "Anônimo"
	}
	if s.Categoria == "" {
		s.Categoria = "Geral"
	}

	sqlStatement := `INSERT INTO sugestoes (texto, author, categoria) VALUES ($1, $2, $3) RETURNING id, data_criacao`
	err := h.DB.QueryRow(sqlStatement, s.Texto, s.Author, s.Categoria).Scan(&s.ID, &s.CreatedAt)
	if err != nil {
		log.Println("Erro Insert:", err)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"erro": "Erro ao salvar"})
	}

	if h.OnCreate != nil {
		go h.OnCreate(s)
	}

	return c.Status(fiber.StatusCreated).JSON(s)
}
