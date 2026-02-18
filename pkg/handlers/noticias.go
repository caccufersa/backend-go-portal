package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cacc/pkg/cache"
	"cacc/pkg/models"

	"github.com/gofiber/fiber/v2"
	"github.com/lib/pq"
)

type NoticiasHandler struct {
	db    *sql.DB
	redis *cache.Redis

	stmtList      *sql.Stmt
	stmtListByCat *sql.Stmt
	stmtGet       *sql.Stmt
	stmtDestaques *sql.Stmt
	stmtInsert    *sql.Stmt
	stmtDelete    *sql.Stmt
}

func NewNoticias(db *sql.DB, r *cache.Redis) *NoticiasHandler {
	n := &NoticiasHandler{db: db, redis: r}
	n.prepare()
	return n
}

func (n *NoticiasHandler) prepare() {
	n.stmtList, _ = n.db.Prepare(`
		SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
		       COALESCE(tags, '{}'), created_at, updated_at
		FROM noticias
		ORDER BY destaque DESC, created_at DESC
		LIMIT $1 OFFSET $2
	`)
	n.stmtListByCat, _ = n.db.Prepare(`
		SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
		       COALESCE(tags, '{}'), created_at, updated_at
		FROM noticias WHERE categoria = $1
		ORDER BY destaque DESC, created_at DESC
		LIMIT $2 OFFSET $3
	`)
	n.stmtGet, _ = n.db.Prepare(`
		SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
		       COALESCE(tags, '{}'), created_at, updated_at
		FROM noticias WHERE id = $1
	`)
	n.stmtDestaques, _ = n.db.Prepare(`
		SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
		       COALESCE(tags, '{}'), created_at, updated_at
		FROM noticias WHERE destaque = true
		ORDER BY created_at DESC LIMIT 10
	`)
	n.stmtInsert, _ = n.db.Prepare(`
		INSERT INTO noticias (titulo, conteudo, resumo, author, categoria, image_url, destaque, tags)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
		          COALESCE(tags, '{}'), created_at, updated_at
	`)
	n.stmtDelete, _ = n.db.Prepare(`DELETE FROM noticias WHERE id = $1`)
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

	cacheKey := fmt.Sprintf("noticias:list:%s:%d:%d", categoria, limit, offset)
	var cached []models.Noticia
	if n.redis.Get(cacheKey, &cached) {
		return c.JSON(cached)
	}

	var rows *sql.Rows
	var err error

	if categoria != "" {
		rows, err = n.stmtListByCat.Query(categoria, limit, offset)
	} else {
		rows, err = n.stmtList.Query(limit, offset)
	}
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao buscar notícias"})
	}
	defer rows.Close()

	noticias := n.scanNoticias(rows)
	n.redis.Set(cacheKey, noticias, 30*time.Second)
	return c.JSON(noticias)
}

// GET /noticias/:id
func (n *NoticiasHandler) BuscarPorID(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil || id <= 0 {
		return c.Status(400).JSON(fiber.Map{"erro": "ID inválido"})
	}

	cacheKey := fmt.Sprintf("noticias:item:%d", id)
	var cached models.Noticia
	if n.redis.Get(cacheKey, &cached) {
		return c.JSON(cached)
	}

	var noticia models.Noticia
	var tags pq.StringArray
	err = n.stmtGet.QueryRow(id).Scan(
		&noticia.ID, &noticia.Titulo, &noticia.Conteudo, &noticia.Resumo, &noticia.Author,
		&noticia.Categoria, &noticia.ImageURL, &noticia.Destaque, &tags, &noticia.CreatedAt, &noticia.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return c.Status(404).JSON(fiber.Map{"erro": "Notícia não encontrada"})
	}
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro interno"})
	}

	noticia.Tags = tags
	n.parseEditorJS(&noticia)
	n.redis.Set(cacheKey, noticia, time.Minute)
	return c.JSON(noticia)
}

// GET /noticias/destaques
func (n *NoticiasHandler) Destaques(c *fiber.Ctx) error {
	var cached []models.Noticia
	if n.redis.Get("noticias:destaques", &cached) {
		return c.JSON(cached)
	}

	rows, err := n.stmtDestaques.Query()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao buscar destaques"})
	}
	defer rows.Close()

	noticias := n.scanNoticias(rows)
	n.redis.Set("noticias:destaques", noticias, 30*time.Second)
	return c.JSON(noticias)
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

	conteudoStr, err := models.ParseConteudo(req.Conteudo)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "Formato de conteúdo inválido"})
	}
	conteudoStr = strings.TrimSpace(conteudoStr)
	if conteudoStr == "" {
		return c.Status(400).JSON(fiber.Map{"erro": "Conteúdo não pode ser vazio"})
	}

	if req.Categoria == "" {
		req.Categoria = "Geral"
	}
	if req.Resumo == "" {
		req.Resumo = gerarResumo(conteudoStr)
	}

	username, _ := c.Locals("username").(string)
	if req.Author == "" {
		if username != "" {
			req.Author = username
		} else {
			req.Author = "Anônimo"
		}
	}

	var noticia models.Noticia
	var tags pq.StringArray
	err = n.stmtInsert.QueryRow(
		req.Titulo, conteudoStr, req.Resumo, req.Author, req.Categoria,
		req.ImageURL, req.Destaque, pq.Array(req.Tags),
	).Scan(
		&noticia.ID, &noticia.Titulo, &noticia.Conteudo, &noticia.Resumo, &noticia.Author,
		&noticia.Categoria, &noticia.ImageURL, &noticia.Destaque, &tags, &noticia.CreatedAt, &noticia.UpdatedAt,
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao criar notícia"})
	}

	noticia.Tags = tags
	n.parseEditorJS(&noticia)
	n.invalidateCache()

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

	sets := []string{}
	args := []interface{}{}
	argIdx := 1

	if req.Titulo != nil {
		sets = append(sets, "titulo = $"+strconv.Itoa(argIdx))
		args = append(args, *req.Titulo)
		argIdx++
	}
	if req.Conteudo != nil {
		conteudoStr, err := models.ParseConteudo(req.Conteudo)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"erro": "Formato de conteúdo inválido"})
		}
		sets = append(sets, "conteudo = $"+strconv.Itoa(argIdx))
		args = append(args, conteudoStr)
		argIdx++
	}
	if req.Resumo != nil {
		sets = append(sets, "resumo = $"+strconv.Itoa(argIdx))
		args = append(args, *req.Resumo)
		argIdx++
	}
	if req.Categoria != nil {
		sets = append(sets, "categoria = $"+strconv.Itoa(argIdx))
		args = append(args, *req.Categoria)
		argIdx++
	}
	if req.ImageURL != nil {
		sets = append(sets, "image_url = $"+strconv.Itoa(argIdx))
		args = append(args, *req.ImageURL)
		argIdx++
	}
	if req.Destaque != nil {
		sets = append(sets, "destaque = $"+strconv.Itoa(argIdx))
		args = append(args, *req.Destaque)
		argIdx++
	}
	if req.Tags != nil {
		sets = append(sets, "tags = $"+strconv.Itoa(argIdx))
		args = append(args, pq.Array(req.Tags))
		argIdx++
	}

	if len(sets) == 0 {
		return c.Status(400).JSON(fiber.Map{"erro": "Nenhum campo para atualizar"})
	}

	sets = append(sets, "updated_at = NOW()")
	query := "UPDATE noticias SET " + strings.Join(sets, ", ") + " WHERE id = $" + strconv.Itoa(argIdx)
	args = append(args, id)

	result, err := n.db.Exec(query, args...)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao atualizar"})
	}

	rowsAff, _ := result.RowsAffected()
	if rowsAff == 0 {
		return c.Status(404).JSON(fiber.Map{"erro": "Notícia não encontrada"})
	}

	// Re-fetch updated noticia
	var noticia models.Noticia
	var tags pq.StringArray
	n.stmtGet.QueryRow(id).Scan(
		&noticia.ID, &noticia.Titulo, &noticia.Conteudo, &noticia.Resumo, &noticia.Author,
		&noticia.Categoria, &noticia.ImageURL, &noticia.Destaque, &tags, &noticia.CreatedAt, &noticia.UpdatedAt,
	)
	noticia.Tags = tags
	n.parseEditorJS(&noticia)
	n.invalidateCache()
	n.redis.Del(fmt.Sprintf("noticias:item:%d", id))

	return c.JSON(noticia)
}

// DELETE /noticias/:id (auth required)
func (n *NoticiasHandler) Deletar(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil || id <= 0 {
		return c.Status(400).JSON(fiber.Map{"erro": "ID inválido"})
	}

	result, err := n.stmtDelete.Exec(id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao deletar"})
	}

	rowsAff, _ := result.RowsAffected()
	if rowsAff == 0 {
		return c.Status(404).JSON(fiber.Map{"erro": "Notícia não encontrada"})
	}

	n.invalidateCache()
	n.redis.Del(fmt.Sprintf("noticias:item:%d", id))

	return c.JSON(fiber.Map{"id": id, "status": "deleted"})
}

// ──────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────

func (n *NoticiasHandler) scanNoticias(rows *sql.Rows) []models.Noticia {
	noticias := []models.Noticia{}
	for rows.Next() {
		var noticia models.Noticia
		var tags pq.StringArray
		if err := rows.Scan(
			&noticia.ID, &noticia.Titulo, &noticia.Conteudo, &noticia.Resumo, &noticia.Author,
			&noticia.Categoria, &noticia.ImageURL, &noticia.Destaque, &tags, &noticia.CreatedAt, &noticia.UpdatedAt,
		); err != nil {
			continue
		}
		noticia.Tags = tags
		n.parseEditorJS(&noticia)
		noticias = append(noticias, noticia)
	}
	return noticias
}

func (n *NoticiasHandler) parseEditorJS(noticia *models.Noticia) {
	var editorData models.EditorJSData
	if err := json.Unmarshal([]byte(noticia.Conteudo), &editorData); err == nil {
		noticia.ConteudoObj = &editorData
	}
}

func (n *NoticiasHandler) invalidateCache() {
	n.redis.DelPattern("noticias:*")
}

func gerarResumo(conteudoStr string) string {
	var editorData models.EditorJSData
	if err := json.Unmarshal([]byte(conteudoStr), &editorData); err == nil {
		resumoText := ""
		for _, block := range editorData.Blocks {
			if blockType, ok := block["type"].(string); ok && (blockType == "paragraph" || blockType == "header") {
				if data, ok := block["data"].(map[string]interface{}); ok {
					if text, ok := data["text"].(string); ok {
						resumoText += text + " "
						if len(resumoText) > 200 {
							break
						}
					}
				}
			}
		}
		if len(resumoText) > 200 {
			return resumoText[:200] + "..."
		}
		if resumoText != "" {
			return resumoText
		}
		return "Nova notícia"
	}
	if len(conteudoStr) > 200 {
		return conteudoStr[:200] + "..."
	}
	return conteudoStr
}
