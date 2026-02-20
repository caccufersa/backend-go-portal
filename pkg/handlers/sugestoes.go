package handlers

import (
	"cacc/pkg/services"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type SugestoesHandler struct {
	service services.SugestoesService
}

func NewSugestoes(service services.SugestoesService) *SugestoesHandler {
	return &SugestoesHandler{service: service}
}

// GET /sugestoes
func (sg *SugestoesHandler) Listar(c *fiber.Ctx) error {
	lista, err := sg.service.Listar()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao buscar sugestões"})
	}

	return c.JSON(lista)
}

// POST /sugestoes (auth required)
func (sg *SugestoesHandler) Criar(c *fiber.Ctx) error {
	var req struct {
		Texto     string `json:"texto"`
		Categoria string `json:"categoria"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}

	texto := strings.TrimSpace(req.Texto)
	if texto == "" {
		return c.Status(400).JSON(fiber.Map{"erro": "Texto não pode ser vazio"})
	}
	if len(texto) > 2000 {
		return c.Status(400).JSON(fiber.Map{"erro": "Texto muito longo (max 2000)"})
	}

	username, _ := c.Locals("username").(string)
	if username == "" {
		username = "Anônimo"
	}

	categoria := strings.TrimSpace(req.Categoria)
	if categoria == "" {
		categoria = "Geral"
	}

	sugestao, err := sg.service.Criar(texto, username, categoria)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao salvar"})
	}

	return c.Status(201).JSON(sugestao)
}

// DELETE /sugestoes/:id (admin)
func (sg *SugestoesHandler) Deletar(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "ID Invalido"})
	}

	err = sg.service.Deletar(id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro interno"})
	}
	return c.JSON(fiber.Map{"status": "deleted"})
}

// PUT /sugestoes/:id (admin)
func (sg *SugestoesHandler) Atualizar(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "ID Invalido"})
	}

	var req struct {
		Texto     string `json:"texto"`
		Categoria string `json:"categoria"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}

	err = sg.service.Atualizar(id, req.Texto, req.Categoria)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro interno"})
	}
	return c.JSON(fiber.Map{"status": "updated"})
}
