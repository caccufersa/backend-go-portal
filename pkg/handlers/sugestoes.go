package handlers

import (
	"database/sql"
	"strings"
	"time"

	"cacc/pkg/cache"
	"cacc/pkg/models"

	"github.com/gofiber/fiber/v2"
)

type SugestoesHandler struct {
	db    *sql.DB
	redis *cache.Redis

	stmtList   *sql.Stmt
	stmtInsert *sql.Stmt
}

func NewSugestoes(db *sql.DB, r *cache.Redis) *SugestoesHandler {
	sg := &SugestoesHandler{db: db, redis: r}
	sg.prepare()
	return sg
}

func (sg *SugestoesHandler) prepare() {
	sg.stmtList, _ = sg.db.Prepare(`
		SELECT id, texto, data_criacao, COALESCE(author, 'Anônimo'), COALESCE(categoria, 'Geral')
		FROM sugestoes ORDER BY id DESC LIMIT 200
	`)
	sg.stmtInsert, _ = sg.db.Prepare(`
		INSERT INTO sugestoes (texto, author, categoria)
		VALUES ($1, $2, $3)
		RETURNING id, data_criacao
	`)
}

// GET /sugestoes
func (sg *SugestoesHandler) Listar(c *fiber.Ctx) error {
	var cached []models.Sugestao
	if sg.redis.Get("sugestoes:all", &cached) {
		return c.JSON(cached)
	}

	rows, err := sg.stmtList.Query()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro no banco"})
	}
	defer rows.Close()

	lista := []models.Sugestao{}
	for rows.Next() {
		var s models.Sugestao
		if err := rows.Scan(&s.ID, &s.Texto, &s.CreatedAt, &s.Author, &s.Categoria); err != nil {
			continue
		}
		lista = append(lista, s)
	}

	sg.redis.Set("sugestoes:all", lista, 30*time.Second)
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

	var s models.Sugestao
	err := sg.stmtInsert.QueryRow(texto, username, categoria).Scan(&s.ID, &s.CreatedAt)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao salvar"})
	}

	s.Texto = texto
	s.Author = username
	s.Categoria = categoria

	sg.redis.Del("sugestoes:all")
	return c.Status(201).JSON(s)
}
