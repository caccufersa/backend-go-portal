package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"strconv"
	"strings"

	"cacc/pkg/broker"
	"cacc/pkg/envelope"
	"cacc/services/noticias/models"

	"github.com/lib/pq"
)

type Handler struct {
	DB     *sql.DB
	broker *broker.Broker
}

func New(db *sql.DB, b *broker.Broker) *Handler {
	return &Handler{DB: db, broker: b}
}

func (h *Handler) RegisterActions() {
	h.broker.On("noticias.list", h.listar)
	h.broker.On("noticias.get", h.buscarPorID)
	h.broker.On("noticias.destaques", h.destaques)
	h.broker.On("noticias.create", h.criar)
	h.broker.On("noticias.update", h.atualizar)
	h.broker.On("noticias.delete", h.deletar)
}

func (h *Handler) listar(env envelope.Envelope) {
	type listReq struct {
		Limit     int    `json:"limit"`
		Offset    int    `json:"offset"`
		Categoria string `json:"categoria"`
	}
	req, _ := envelope.ParseData[listReq](env)
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 20
	}

	var rows *sql.Rows
	var err error

	if req.Categoria != "" {
		rows, err = h.DB.Query(
			`SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
			 COALESCE(tags, '{}'), created_at, updated_at
			 FROM noticias WHERE categoria = $1
			 ORDER BY destaque DESC, created_at DESC LIMIT $2 OFFSET $3`,
			req.Categoria, req.Limit, req.Offset,
		)
	} else {
		rows, err = h.DB.Query(
			`SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
			 COALESCE(tags, '{}'), created_at, updated_at
			 FROM noticias
			 ORDER BY destaque DESC, created_at DESC LIMIT $1 OFFSET $2`,
			req.Limit, req.Offset,
		)
	}

	if err != nil {
		log.Println("Erro query noticias:", err)
		h.broker.ReplyError("gateway:replies", env, 500, "Erro ao buscar notícias")
		return
	}
	defer rows.Close()

	noticias := h.scanNoticias(rows)
	h.broker.Reply("gateway:replies", env, noticias)
}

func (h *Handler) buscarPorID(env envelope.Envelope) {
	type getReq struct {
		ID int `json:"id"`
	}
	req, _ := envelope.ParseData[getReq](env)
	if req.ID <= 0 {
		h.broker.ReplyError("gateway:replies", env, 400, "ID inválido")
		return
	}

	var n models.Noticia
	var tags pq.StringArray
	err := h.DB.QueryRow(
		`SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
		 COALESCE(tags, '{}'), created_at, updated_at
		 FROM noticias WHERE id = $1`, req.ID,
	).Scan(&n.ID, &n.Titulo, &n.Conteudo, &n.Resumo, &n.Author,
		&n.Categoria, &n.ImageURL, &n.Destaque, &tags, &n.CreatedAt, &n.UpdatedAt)

	if err == sql.ErrNoRows {
		h.broker.ReplyError("gateway:replies", env, 404, "Notícia não encontrada")
		return
	}
	if err != nil {
		h.broker.ReplyError("gateway:replies", env, 500, "Erro interno")
		return
	}

	n.Tags = tags
	h.parseEditorJS(&n)
	h.broker.Reply("gateway:replies", env, n)
}

func (h *Handler) destaques(env envelope.Envelope) {
	rows, err := h.DB.Query(
		`SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
		 COALESCE(tags, '{}'), created_at, updated_at
		 FROM noticias WHERE destaque = true
		 ORDER BY created_at DESC LIMIT 10`,
	)
	if err != nil {
		h.broker.ReplyError("gateway:replies", env, 500, "Erro ao buscar destaques")
		return
	}
	defer rows.Close()

	noticias := h.scanNoticias(rows)
	h.broker.Reply("gateway:replies", env, noticias)
}

func (h *Handler) criar(env envelope.Envelope) {
	var req models.CriarNoticiaRequest
	if err := json.Unmarshal(env.Data, &req); err != nil {
		h.broker.ReplyError("gateway:replies", env, 400, "JSON inválido")
		return
	}

	req.Titulo = strings.TrimSpace(req.Titulo)
	if req.Titulo == "" || req.Conteudo == nil {
		h.broker.ReplyError("gateway:replies", env, 400, "Título e conteúdo são obrigatórios")
		return
	}

	conteudoStr, err := models.ParseConteudo(req.Conteudo)
	if err != nil {
		h.broker.ReplyError("gateway:replies", env, 400, "Formato de conteúdo inválido")
		return
	}

	conteudoStr = strings.TrimSpace(conteudoStr)
	if conteudoStr == "" {
		h.broker.ReplyError("gateway:replies", env, 400, "Conteúdo não pode ser vazio")
		return
	}

	if req.Categoria == "" {
		req.Categoria = "Geral"
	}

	if req.Resumo == "" {
		req.Resumo = h.gerarResumo(conteudoStr)
	}

	if req.Author == "" {
		if env.Username != "" {
			req.Author = env.Username
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
		h.broker.ReplyError("gateway:replies", env, 500, "Erro ao criar notícia")
		return
	}

	n.Tags = tags
	h.parseEditorJS(&n)

	h.broker.Reply("gateway:replies", env, n)
	h.broker.Broadcast("gateway:broadcast", "new_noticia", "noticias", n)
}

func (h *Handler) atualizar(env envelope.Envelope) {
	type updateEnv struct {
		ID int `json:"id"`
		models.AtualizarNoticiaRequest
	}
	var req updateEnv
	if err := json.Unmarshal(env.Data, &req); err != nil {
		h.broker.ReplyError("gateway:replies", env, 400, "JSON inválido")
		return
	}

	if req.ID <= 0 {
		h.broker.ReplyError("gateway:replies", env, 400, "ID inválido")
		return
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
			h.broker.ReplyError("gateway:replies", env, 400, "Formato de conteúdo inválido")
			return
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
		h.broker.ReplyError("gateway:replies", env, 400, "Nenhum campo para atualizar")
		return
	}

	sets = append(sets, "updated_at = NOW()")
	query := "UPDATE noticias SET " + strings.Join(sets, ", ") + " WHERE id = $" + strconv.Itoa(argIdx)
	args = append(args, req.ID)

	result, err := h.DB.Exec(query, args...)
	if err != nil {
		log.Println("Erro update noticia:", err)
		h.broker.ReplyError("gateway:replies", env, 500, "Erro ao atualizar")
		return
	}

	rowsAff, _ := result.RowsAffected()
	if rowsAff == 0 {
		h.broker.ReplyError("gateway:replies", env, 404, "Notícia não encontrada")
		return
	}

	var n models.Noticia
	var tags pq.StringArray
	h.DB.QueryRow(
		`SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
		 COALESCE(tags, '{}'), created_at, updated_at
		 FROM noticias WHERE id = $1`, req.ID,
	).Scan(&n.ID, &n.Titulo, &n.Conteudo, &n.Resumo, &n.Author,
		&n.Categoria, &n.ImageURL, &n.Destaque, &tags, &n.CreatedAt, &n.UpdatedAt)
	n.Tags = tags
	h.parseEditorJS(&n)

	h.broker.Reply("gateway:replies", env, n)
}

func (h *Handler) deletar(env envelope.Envelope) {
	type deleteReq struct {
		ID int `json:"id"`
	}
	req, _ := envelope.ParseData[deleteReq](env)
	if req.ID <= 0 {
		h.broker.ReplyError("gateway:replies", env, 400, "ID inválido")
		return
	}

	result, err := h.DB.Exec(`DELETE FROM noticias WHERE id = $1`, req.ID)
	if err != nil {
		h.broker.ReplyError("gateway:replies", env, 500, "Erro ao deletar")
		return
	}

	rowsAff, _ := result.RowsAffected()
	if rowsAff == 0 {
		h.broker.ReplyError("gateway:replies", env, 404, "Notícia não encontrada")
		return
	}

	payload := map[string]interface{}{"id": req.ID, "status": "ok"}
	h.broker.Reply("gateway:replies", env, payload)
	h.broker.Broadcast("gateway:broadcast", "noticia_deleted", "noticias", map[string]int{"id": req.ID})
}

func (h *Handler) scanNoticias(rows *sql.Rows) []models.Noticia {
	noticias := []models.Noticia{}
	for rows.Next() {
		var n models.Noticia
		var tags pq.StringArray
		if err := rows.Scan(&n.ID, &n.Titulo, &n.Conteudo, &n.Resumo, &n.Author,
			&n.Categoria, &n.ImageURL, &n.Destaque, &tags, &n.CreatedAt, &n.UpdatedAt); err != nil {
			continue
		}
		n.Tags = tags
		h.parseEditorJS(&n)
		noticias = append(noticias, n)
	}
	return noticias
}

func (h *Handler) parseEditorJS(n *models.Noticia) {
	var editorData models.EditorJSData
	if err := json.Unmarshal([]byte(n.Conteudo), &editorData); err == nil {
		n.ConteudoObj = &editorData
	}
}

func (h *Handler) gerarResumo(conteudoStr string) string {
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
