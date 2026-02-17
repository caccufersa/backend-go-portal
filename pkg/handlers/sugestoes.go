package handlers

import (
	"database/sql"

	"cacc/pkg/cache"
	"cacc/pkg/envelope"
	"cacc/pkg/hub"
	"cacc/pkg/models"
)

type SugestoesHandler struct {
	db    *sql.DB
	hub   *hub.Hub
	redis *cache.Redis
}

func NewSugestoes(db *sql.DB, h *hub.Hub, r *cache.Redis) *SugestoesHandler {
	return &SugestoesHandler{db: db, hub: h, redis: r}
}

func (sg *SugestoesHandler) RegisterActions() {
	sg.hub.On("sugestoes.list", sg.listar)
	sg.hub.On("sugestoes.create", sg.criar)
}

func (sg *SugestoesHandler) listar(env envelope.Envelope) {
	var cached []models.Sugestao
	if sg.redis.Get("sugestoes:all", &cached) {
		sg.hub.Reply(env, cached)
		return
	}

	rows, err := sg.db.Query(
		`SELECT id, texto, data_criacao, COALESCE(author, 'Anônimo'), COALESCE(categoria, 'Geral')
		 FROM sugestoes ORDER BY id DESC`,
	)
	if err != nil {
		sg.hub.ReplyError(env, 500, "Erro no banco")
		return
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

	sg.redis.Set("sugestoes:all", lista, 30e9)
	sg.hub.Reply(env, lista)
}

func (sg *SugestoesHandler) criar(env envelope.Envelope) {
	req, err := envelope.ParseData[models.Sugestao](env)
	if err != nil {
		sg.hub.ReplyError(env, 400, "JSON inválido")
		return
	}

	if req.Author == "" {
		if env.Username != "" {
			req.Author = env.Username
		} else {
			req.Author = "Anônimo"
		}
	}
	if req.Categoria == "" {
		req.Categoria = "Geral"
	}

	err = sg.db.QueryRow(
		`INSERT INTO sugestoes (texto, author, categoria) VALUES ($1, $2, $3) RETURNING id, data_criacao`,
		req.Texto, req.Author, req.Categoria,
	).Scan(&req.ID, &req.CreatedAt)

	if err != nil {
		sg.hub.ReplyError(env, 500, "Erro ao salvar")
		return
	}

	sg.redis.Del("sugestoes:all")
	sg.hub.Reply(env, req)
	sg.hub.Broadcast("new_sugestao", "sugestoes", req)
}
