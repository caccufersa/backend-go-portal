package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"strconv"
	"strings"

	"cacc/services/noticias/models"

	"github.com/gofiber/fiber/v2"
	"github.com/lib/pq"
)

type Handler struct {
	DB          *sql.DB
	OnBroadcast func(msgType string, data interface{})
}

func New(db *sql.DB) *Handler {
	return &Handler{DB: db}
}

func (h *Handler) Listar(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 20)
	if limit > 100 {
		limit = 100
	}
	offset := c.QueryInt("offset", 0)
	categoria := c.Query("categoria")

	var rows *sql.Rows
	var err error

	if categoria != "" {
		rows, err = h.DB.Query(
			`SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque, 
			 COALESCE(tags, '{}'), created_at, updated_at
			 FROM noticias WHERE categoria = $1
			 ORDER BY destaque DESC, created_at DESC LIMIT $2 OFFSET $3`,
			categoria, limit, offset,
		)
	} else {
		rows, err = h.DB.Query(
			`SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
			 COALESCE(tags, '{}'), created_at, updated_at
			 FROM noticias
			 ORDER BY destaque DESC, created_at DESC LIMIT $1 OFFSET $2`,
			limit, offset,
		)
	}

	if err != nil {
		log.Println("Erro query noticias:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao buscar notícias"})
	}
	defer rows.Close()

	noticias := []models.Noticia{}
	for rows.Next() {
		var n models.Noticia
		var tags pq.StringArray
		if err := rows.Scan(&n.ID, &n.Titulo, &n.Conteudo, &n.Resumo, &n.Author,
			&n.Categoria, &n.ImageURL, &n.Destaque, &tags, &n.CreatedAt, &n.UpdatedAt); err != nil {
			log.Println("Erro scan noticia:", err)
			continue
		}
		n.Tags = tags

		var editorData models.EditorJSData
		if err := json.Unmarshal([]byte(n.Conteudo), &editorData); err == nil {
			n.ConteudoObj = &editorData
		}

		noticias = append(noticias, n)
	}

	return c.JSON(noticias)
}

func (h *Handler) BuscarPorID(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "ID inválido"})
	}

	var n models.Noticia
	var tags pq.StringArray
	err = h.DB.QueryRow(
		`SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque, 
		 COALESCE(tags, '{}'), created_at, updated_at
		 FROM noticias WHERE id = $1`, id,
	).Scan(&n.ID, &n.Titulo, &n.Conteudo, &n.Resumo, &n.Author,
		&n.Categoria, &n.ImageURL, &n.Destaque, &tags, &n.CreatedAt, &n.UpdatedAt)

	if err == sql.ErrNoRows {
		return c.Status(404).JSON(fiber.Map{"erro": "Notícia não encontrada"})
	}
	if err != nil {
		log.Println("Erro query noticia:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro interno"})
	}

	n.Tags = tags

	var editorData models.EditorJSData
	if err := json.Unmarshal([]byte(n.Conteudo), &editorData); err == nil {
		n.ConteudoObj = &editorData
	}

	return c.JSON(n)
}

func (h *Handler) Destaques(c *fiber.Ctx) error {
	rows, err := h.DB.Query(
		`SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque, 
		 COALESCE(tags, '{}'), created_at, updated_at
		 FROM noticias WHERE destaque = true
		 ORDER BY created_at DESC LIMIT 10`,
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao buscar destaques"})
	}
	defer rows.Close()

	noticias := []models.Noticia{}
	for rows.Next() {
		var n models.Noticia
		var tags pq.StringArray
		if err := rows.Scan(&n.ID, &n.Titulo, &n.Conteudo, &n.Resumo, &n.Author,
			&n.Categoria, &n.ImageURL, &n.Destaque, &tags, &n.CreatedAt, &n.UpdatedAt); err != nil {
			continue
		}
		n.Tags = tags

		var editorData models.EditorJSData
		if err := json.Unmarshal([]byte(n.Conteudo), &editorData); err == nil {
			n.ConteudoObj = &editorData
		}

		noticias = append(noticias, n)
	}

	return c.JSON(noticias)
}

func (h *Handler) Criar(c *fiber.Ctx) error {
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
				req.Resumo = resumoText[:200] + "..."
			} else if resumoText != "" {
				req.Resumo = resumoText
			} else {
				req.Resumo = "Nova notícia"
			}
		} else {
			if len(conteudoStr) > 200 {
				req.Resumo = conteudoStr[:200] + "..."
			} else {
				req.Resumo = conteudoStr
			}
		}
	}

	if req.Author == "" {
		if username, ok := c.Locals("username").(string); ok {
			req.Author = username
		} else {
			req.Author = "Anônimo"
		}
	}

	var n models.Noticia
	var tags pq.StringArray
	err = h.DB.QueryRow(
		`INSERT INTO noticias (titulo, conteudo, resumo, author, categoria, image_url, destaque, tags)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque, 
		 COALESCE(tags, '{}'), created_at, updated_at`,
		req.Titulo, conteudoStr, req.Resumo, req.Author, req.Categoria, req.ImageURL, req.Destaque, pq.Array(req.Tags),
	).Scan(&n.ID, &n.Titulo, &n.Conteudo, &n.Resumo, &n.Author,
		&n.Categoria, &n.ImageURL, &n.Destaque, &tags, &n.CreatedAt, &n.UpdatedAt)

	if err != nil {
		log.Println("Erro insert noticia:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao criar notícia"})
	}

	n.Tags = tags

	var editorData models.EditorJSData
	if err := json.Unmarshal([]byte(n.Conteudo), &editorData); err == nil {
		n.ConteudoObj = &editorData
	}

	if h.OnBroadcast != nil {
		go h.OnBroadcast("new_noticia", n)
	}

	return c.Status(201).JSON(n)
}

func (h *Handler) Atualizar(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
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

	result, err := h.DB.Exec(query, args...)
	if err != nil {
		log.Println("Erro update noticia:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao atualizar"})
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return c.Status(404).JSON(fiber.Map{"erro": "Notícia não encontrada"})
	}

	return h.BuscarPorID(c)
}

func (h *Handler) Deletar(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "ID inválido"})
	}

	result, err := h.DB.Exec(`DELETE FROM noticias WHERE id = $1`, id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao deletar"})
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return c.Status(404).JSON(fiber.Map{"erro": "Notícia não encontrada"})
	}

	if h.OnBroadcast != nil {
		go h.OnBroadcast("noticia_deleted", fiber.Map{"id": id})
	}

	return c.JSON(fiber.Map{"status": "ok", "message": "Notícia deletada"})
}
