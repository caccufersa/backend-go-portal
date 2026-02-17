package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cacc/pkg/cache"
	"cacc/pkg/envelope"
	"cacc/pkg/hub"
	"cacc/pkg/models"

	"github.com/lib/pq"
)

type NoticiasHandler struct {
	db    *sql.DB
	hub   *hub.Hub
	redis *cache.Redis
}

func NewNoticias(db *sql.DB, h *hub.Hub, r *cache.Redis) *NoticiasHandler {
	return &NoticiasHandler{db: db, hub: h, redis: r}
}

func (n *NoticiasHandler) RegisterActions() {
	n.hub.On("noticias.list", n.listar)
	n.hub.On("noticias.get", n.buscarPorID)
	n.hub.On("noticias.destaques", n.destaques)
	n.hub.On("noticias.create", n.criar)
	n.hub.On("noticias.update", n.atualizar)
	n.hub.On("noticias.delete", n.deletar)
}

func (n *NoticiasHandler) listar(env envelope.Envelope) {
	type listReq struct {
		Limit     int    `json:"limit"`
		Offset    int    `json:"offset"`
		Categoria string `json:"categoria"`
	}
	req, _ := envelope.ParseData[listReq](env)
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 20
	}

	cacheKey := fmt.Sprintf("noticias:%s:%d:%d", req.Categoria, req.Limit, req.Offset)
	var cached []models.Noticia
	if n.redis.Get(cacheKey, &cached) {
		n.hub.Reply(env, cached)
		return
	}

	var rows *sql.Rows
	var err error

	if req.Categoria != "" {
		rows, err = n.db.Query(
			`SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
			 COALESCE(tags, '{}'), created_at, updated_at
			 FROM noticias WHERE categoria = $1
			 ORDER BY destaque DESC, created_at DESC LIMIT $2 OFFSET $3`,
			req.Categoria, req.Limit, req.Offset,
		)
	} else {
		rows, err = n.db.Query(
			`SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
			 COALESCE(tags, '{}'), created_at, updated_at
			 FROM noticias
			 ORDER BY destaque DESC, created_at DESC LIMIT $1 OFFSET $2`,
			req.Limit, req.Offset,
		)
	}

	if err != nil {
		n.hub.ReplyError(env, 500, "Erro ao buscar notícias")
		return
	}
	defer rows.Close()

	noticias := n.scanNoticias(rows)
	n.redis.Set(cacheKey, noticias, 30*time.Second)
	n.hub.Reply(env, noticias)
}

func (n *NoticiasHandler) buscarPorID(env envelope.Envelope) {
	type getReq struct {
		ID int `json:"id"`
	}
	req, _ := envelope.ParseData[getReq](env)
	if req.ID <= 0 {
		n.hub.ReplyError(env, 400, "ID inválido")
		return
	}

	cacheKey := fmt.Sprintf("noticia:%d", req.ID)
	var cached models.Noticia
	if n.redis.Get(cacheKey, &cached) {
		n.hub.Reply(env, cached)
		return
	}

	var noticia models.Noticia
	var tags pq.StringArray
	err := n.db.QueryRow(
		`SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
		 COALESCE(tags, '{}'), created_at, updated_at
		 FROM noticias WHERE id = $1`, req.ID,
	).Scan(&noticia.ID, &noticia.Titulo, &noticia.Conteudo, &noticia.Resumo, &noticia.Author,
		&noticia.Categoria, &noticia.ImageURL, &noticia.Destaque, &tags, &noticia.CreatedAt, &noticia.UpdatedAt)

	if err == sql.ErrNoRows {
		n.hub.ReplyError(env, 404, "Notícia não encontrada")
		return
	}
	if err != nil {
		n.hub.ReplyError(env, 500, "Erro interno")
		return
	}

	noticia.Tags = tags
	n.parseEditorJS(&noticia)
	n.redis.Set(cacheKey, noticia, time.Minute)
	n.hub.Reply(env, noticia)
}

func (n *NoticiasHandler) destaques(env envelope.Envelope) {
	var cached []models.Noticia
	if n.redis.Get("noticias:destaques", &cached) {
		n.hub.Reply(env, cached)
		return
	}

	rows, err := n.db.Query(
		`SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
		 COALESCE(tags, '{}'), created_at, updated_at
		 FROM noticias WHERE destaque = true
		 ORDER BY created_at DESC LIMIT 10`,
	)
	if err != nil {
		n.hub.ReplyError(env, 500, "Erro ao buscar destaques")
		return
	}
	defer rows.Close()

	noticias := n.scanNoticias(rows)
	n.redis.Set("noticias:destaques", noticias, 30*time.Second)
	n.hub.Reply(env, noticias)
}

func (n *NoticiasHandler) criar(env envelope.Envelope) {
	var req models.CriarNoticiaRequest
	if err := json.Unmarshal(env.Data, &req); err != nil {
		n.hub.ReplyError(env, 400, "JSON inválido")
		return
	}

	req.Titulo = strings.TrimSpace(req.Titulo)
	if req.Titulo == "" || req.Conteudo == nil {
		n.hub.ReplyError(env, 400, "Título e conteúdo são obrigatórios")
		return
	}

	conteudoStr, err := models.ParseConteudo(req.Conteudo)
	if err != nil {
		n.hub.ReplyError(env, 400, "Formato de conteúdo inválido")
		return
	}

	conteudoStr = strings.TrimSpace(conteudoStr)
	if conteudoStr == "" {
		n.hub.ReplyError(env, 400, "Conteúdo não pode ser vazio")
		return
	}

	if req.Categoria == "" {
		req.Categoria = "Geral"
	}
	if req.Resumo == "" {
		req.Resumo = n.gerarResumo(conteudoStr)
	}
	if req.Author == "" {
		if env.Username != "" {
			req.Author = env.Username
		} else {
			req.Author = "Anônimo"
		}
	}

	var noticia models.Noticia
	var tags pq.StringArray
	err = n.db.QueryRow(
		`INSERT INTO noticias (titulo, conteudo, resumo, author, categoria, image_url, destaque, tags)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
		 COALESCE(tags, '{}'), created_at, updated_at`,
		req.Titulo, conteudoStr, req.Resumo, req.Author, req.Categoria, req.ImageURL, req.Destaque, pq.Array(req.Tags),
	).Scan(&noticia.ID, &noticia.Titulo, &noticia.Conteudo, &noticia.Resumo, &noticia.Author,
		&noticia.Categoria, &noticia.ImageURL, &noticia.Destaque, &tags, &noticia.CreatedAt, &noticia.UpdatedAt)

	if err != nil {
		n.hub.ReplyError(env, 500, "Erro ao criar notícia")
		return
	}

	noticia.Tags = tags
	n.parseEditorJS(&noticia)

	n.redis.DelPattern("noticias:*")
	n.hub.Reply(env, noticia)
	n.hub.Broadcast("new_noticia", "noticias", noticia)
}

func (n *NoticiasHandler) atualizar(env envelope.Envelope) {
	type updateEnv struct {
		ID int `json:"id"`
		models.AtualizarNoticiaRequest
	}
	var req updateEnv
	if err := json.Unmarshal(env.Data, &req); err != nil {
		n.hub.ReplyError(env, 400, "JSON inválido")
		return
	}

	if req.ID <= 0 {
		n.hub.ReplyError(env, 400, "ID inválido")
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
			n.hub.ReplyError(env, 400, "Formato de conteúdo inválido")
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
		n.hub.ReplyError(env, 400, "Nenhum campo para atualizar")
		return
	}

	sets = append(sets, "updated_at = NOW()")
	query := "UPDATE noticias SET " + strings.Join(sets, ", ") + " WHERE id = $" + strconv.Itoa(argIdx)
	args = append(args, req.ID)

	result, err := n.db.Exec(query, args...)
	if err != nil {
		n.hub.ReplyError(env, 500, "Erro ao atualizar")
		return
	}

	rowsAff, _ := result.RowsAffected()
	if rowsAff == 0 {
		n.hub.ReplyError(env, 404, "Notícia não encontrada")
		return
	}

	var noticia models.Noticia
	var tags pq.StringArray
	n.db.QueryRow(
		`SELECT id, titulo, conteudo, resumo, author, categoria, COALESCE(image_url,''), destaque,
		 COALESCE(tags, '{}'), created_at, updated_at
		 FROM noticias WHERE id = $1`, req.ID,
	).Scan(&noticia.ID, &noticia.Titulo, &noticia.Conteudo, &noticia.Resumo, &noticia.Author,
		&noticia.Categoria, &noticia.ImageURL, &noticia.Destaque, &tags, &noticia.CreatedAt, &noticia.UpdatedAt)
	noticia.Tags = tags
	n.parseEditorJS(&noticia)

	n.redis.DelPattern("noticias:*")
	n.redis.Del(fmt.Sprintf("noticia:%d", req.ID))
	n.hub.Reply(env, noticia)
}

func (n *NoticiasHandler) deletar(env envelope.Envelope) {
	type deleteReq struct {
		ID int `json:"id"`
	}
	req, _ := envelope.ParseData[deleteReq](env)
	if req.ID <= 0 {
		n.hub.ReplyError(env, 400, "ID inválido")
		return
	}

	result, err := n.db.Exec(`DELETE FROM noticias WHERE id = $1`, req.ID)
	if err != nil {
		n.hub.ReplyError(env, 500, "Erro ao deletar")
		return
	}

	rowsAff, _ := result.RowsAffected()
	if rowsAff == 0 {
		n.hub.ReplyError(env, 404, "Notícia não encontrada")
		return
	}

	n.redis.DelPattern("noticias:*")
	n.redis.Del(fmt.Sprintf("noticia:%d", req.ID))
	n.hub.Reply(env, map[string]interface{}{"id": req.ID, "status": "ok"})
	n.hub.Broadcast("noticia_deleted", "noticias", map[string]int{"id": req.ID})
}

func (n *NoticiasHandler) scanNoticias(rows *sql.Rows) []models.Noticia {
	noticias := []models.Noticia{}
	for rows.Next() {
		var noticia models.Noticia
		var tags pq.StringArray
		if err := rows.Scan(&noticia.ID, &noticia.Titulo, &noticia.Conteudo, &noticia.Resumo, &noticia.Author,
			&noticia.Categoria, &noticia.ImageURL, &noticia.Destaque, &tags, &noticia.CreatedAt, &noticia.UpdatedAt); err != nil {
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

func (n *NoticiasHandler) gerarResumo(conteudoStr string) string {
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
