package handlers

import (
	"database/sql"
	"log"

	"cacc/pkg/broker"
	"cacc/pkg/envelope"
	"cacc/services/sugestoes/models"
)

type SugestaoHandler struct {
	DB     *sql.DB
	broker *broker.Broker
}

func New(db *sql.DB, b *broker.Broker) *SugestaoHandler {
	return &SugestaoHandler{DB: db, broker: b}
}

func (h *SugestaoHandler) RegisterActions() {
	h.broker.On("sugestoes.list", h.listar)
	h.broker.On("sugestoes.create", h.criar)
}

func (h *SugestaoHandler) listar(env envelope.Envelope) {
	rows, err := h.DB.Query(
		`SELECT id, texto, data_criacao, COALESCE(author, 'Anônimo'), COALESCE(categoria, 'Geral')
		 FROM sugestoes ORDER BY id DESC`,
	)
	if err != nil {
		log.Println("Erro Query:", err)
		h.broker.ReplyError("gateway:replies", env, 500, "Erro no banco")
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

	h.broker.Reply("gateway:replies", env, lista)
}

func (h *SugestaoHandler) criar(env envelope.Envelope) {
	req, err := envelope.ParseData[models.Sugestao](env)
	if err != nil {
		h.broker.ReplyError("gateway:replies", env, 400, "JSON inválido")
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

	err = h.DB.QueryRow(
		`INSERT INTO sugestoes (texto, author, categoria) VALUES ($1, $2, $3) RETURNING id, data_criacao`,
		req.Texto, req.Author, req.Categoria,
	).Scan(&req.ID, &req.CreatedAt)

	if err != nil {
		log.Println("Erro Insert:", err)
		h.broker.ReplyError("gateway:replies", env, 500, "Erro ao salvar")
		return
	}

	h.broker.Reply("gateway:replies", env, req)
	h.broker.Broadcast("gateway:broadcast", "new_sugestao", "sugestoes", req)
}
