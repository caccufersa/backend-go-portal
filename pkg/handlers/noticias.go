package handlers

import (
	"cacc/pkg/models"
	"cacc/pkg/services"
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type NoticiasHandler struct {
	service services.NoticiasService
}

func NewNoticias(service services.NoticiasService) *NoticiasHandler {
	return &NoticiasHandler{service: service}
}

// ──────────────────────────────────────────────
// PUBLIC ROUTES (no auth)
// ──────────────────────────────────────────────

// GET /noticias?limit=20&offset=0&categoria=
func (n *NoticiasHandler) Listar(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 20)
	offset := c.QueryInt("offset", 0)
	categoria := c.Query("categoria")

	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	lista, err := n.service.Listar(categoria, limit, offset)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao buscar notícias"})
	}

	return c.JSON(lista)
}

// GET /noticias/:id
func (n *NoticiasHandler) BuscarPorID(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil || id <= 0 {
		return c.Status(400).JSON(fiber.Map{"erro": "ID inválido"})
	}

	noticia, err := n.service.BuscarPorID(id)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return c.Status(404).JSON(fiber.Map{"erro": "Notícia não encontrada"})
		}
		return c.Status(500).JSON(fiber.Map{"erro": "Erro interno"})
	}

	return c.JSON(noticia)
}

// GET /noticias/destaques
func (n *NoticiasHandler) Destaques(c *fiber.Ctx) error {
	lista, err := n.service.Destaques()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao buscar destaques"})
	}

	return c.JSON(lista)
}

// ──────────────────────────────────────────────
// PRIVATE ROUTES (auth required)
// ──────────────────────────────────────────────

// POST /noticias (auth required)
func (n *NoticiasHandler) Criar(c *fiber.Ctx) error {
	var req models.CriarNoticiaRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}

	req.Titulo = strings.TrimSpace(req.Titulo)
	if req.Titulo == "" || req.Conteudo == nil {
		return c.Status(400).JSON(fiber.Map{"erro": "Título e conteúdo são obrigatórios"})
	}

	username, ok := c.Locals("username").(string)
	if !ok {
		username = "Anônimo"
	}

	noticia, err := n.service.Criar(req, username)
	if err != nil {
		if err.Error() == "formato de conteúdo inválido" || err.Error() == "conteúdo não pode ser vazio" {
			return c.Status(400).JSON(fiber.Map{"erro": err.Error()})
		}
		return c.Status(500).JSON(fiber.Map{"erro": fmt.Sprintf("Erro ao criar notícia: %v", err)})
	}

	return c.Status(201).JSON(noticia)
}

// PUT /noticias/:id (auth required)
func (n *NoticiasHandler) Atualizar(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil || id <= 0 {
		return c.Status(400).JSON(fiber.Map{"erro": "ID inválido"})
	}

	var req models.AtualizarNoticiaRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}

	noticia, err := n.service.Atualizar(id, req)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return c.Status(404).JSON(fiber.Map{"erro": "Notícia não encontrada"})
		}
		if err.Error() == "nenhum campo para atualizar" || err.Error() == "formato de conteúdo inválido" {
			return c.Status(400).JSON(fiber.Map{"erro": err.Error()})
		}
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao atualizar"})
	}

	return c.JSON(noticia)
}

// DELETE /noticias/:id (auth required)
func (n *NoticiasHandler) Deletar(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil || id <= 0 {
		return c.Status(400).JSON(fiber.Map{"erro": "ID inválido"})
	}

	deletado, err := n.service.Deletar(id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao deletar"})
	}
	if !deletado {
		return c.Status(404).JSON(fiber.Map{"erro": "Notícia não encontrada"})
	}

	return c.JSON(fiber.Map{"id": id, "status": "deleted"})
}
