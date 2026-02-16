package handlers

import (
	"database/sql"
	"log"

	"cacc/pkg/broker"
	"cacc/pkg/envelope"
	"cacc/services/social/models"
)

type Handler struct {
	db     *sql.DB
	broker *broker.Broker
}

func New(db *sql.DB, b *broker.Broker) *Handler {
	return &Handler{db: db, broker: b}
}

func (h *Handler) RegisterActions() {
	h.broker.On("social.feed", h.listarFeed)
	h.broker.On("social.thread", h.buscarThread)
	h.broker.On("social.profile", h.buscarPerfil)
	h.broker.On("social.post.create", h.criarPost)
	h.broker.On("social.post.comment", h.comentar)
	h.broker.On("social.post.like", h.curtir)
	h.broker.On("social.post.unlike", h.descurtir)
}

func (h *Handler) listarFeed(env envelope.Envelope) {
	type feedReq struct {
		Limit int `json:"limit"`
	}
	req, _ := envelope.ParseData[feedReq](env)
	limit := req.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	rows, err := h.db.Query(
		`SELECT id, texto, author, likes, created_at
		 FROM posts WHERE parent_id IS NULL
		 ORDER BY created_at DESC LIMIT $1`, limit,
	)
	if err != nil {
		log.Println("Erro query feed:", err)
		h.broker.ReplyError("gateway:replies", env, 500, "Erro ao buscar feed")
		return
	}
	defer rows.Close()

	posts := make([]models.Post, 0, limit)
	for rows.Next() {
		var p models.Post
		if err := rows.Scan(&p.ID, &p.Texto, &p.Author, &p.Likes, &p.CreatedAt); err != nil {
			continue
		}
		p.Replies = h.carregarReplies(p.ID, 3)
		posts = append(posts, p)
	}

	h.broker.Reply("gateway:replies", env, posts)
}

func (h *Handler) buscarThread(env envelope.Envelope) {
	type threadReq struct {
		ID int `json:"id"`
	}
	req, _ := envelope.ParseData[threadReq](env)
	if req.ID <= 0 {
		h.broker.ReplyError("gateway:replies", env, 400, "ID inválido")
		return
	}

	var p models.Post
	err := h.db.QueryRow(
		`SELECT id, texto, author, parent_id, likes, created_at FROM posts WHERE id = $1`, req.ID,
	).Scan(&p.ID, &p.Texto, &p.Author, &p.ParentID, &p.Likes, &p.CreatedAt)

	if err == sql.ErrNoRows {
		h.broker.ReplyError("gateway:replies", env, 404, "Post não encontrado")
		return
	}
	if err != nil {
		h.broker.ReplyError("gateway:replies", env, 500, "Erro no banco")
		return
	}

	p.Replies = h.carregarReplies(p.ID, 10)
	h.broker.Reply("gateway:replies", env, p)
}

func (h *Handler) buscarPerfil(env envelope.Envelope) {
	type profileReq struct {
		Username string `json:"username"`
	}
	req, _ := envelope.ParseData[profileReq](env)
	if req.Username == "" {
		h.broker.ReplyError("gateway:replies", env, 400, "Username obrigatório")
		return
	}

	rows, err := h.db.Query(
		`SELECT id, texto, author, parent_id, likes, created_at
		 FROM posts WHERE author = $1
		 ORDER BY created_at DESC LIMIT 100`, req.Username,
	)
	if err != nil {
		h.broker.ReplyError("gateway:replies", env, 500, "Erro ao buscar perfil")
		return
	}
	defer rows.Close()

	posts := []models.Post{}
	totalLikes := 0
	for rows.Next() {
		var p models.Post
		if err := rows.Scan(&p.ID, &p.Texto, &p.Author, &p.ParentID, &p.Likes, &p.CreatedAt); err != nil {
			continue
		}
		totalLikes += p.Likes
		p.Replies = h.carregarReplies(p.ID, 2)
		posts = append(posts, p)
	}

	h.broker.Reply("gateway:replies", env, map[string]interface{}{
		"username":    req.Username,
		"total_posts": len(posts),
		"total_likes": totalLikes,
		"posts":       posts,
	})
}

func (h *Handler) criarPost(env envelope.Envelope) {
	type createReq struct {
		Texto  string `json:"texto"`
		Author string `json:"author"`
	}
	req, _ := envelope.ParseData[createReq](env)

	author := req.Author
	if author == "" {
		author = env.Username
	}
	if author == "" {
		author = "Anônimo"
	}

	var p models.Post
	err := h.db.QueryRow(
		`INSERT INTO posts (texto, author, parent_id, likes)
		 VALUES ($1, $2, NULL, 0)
		 RETURNING id, created_at`, req.Texto, author,
	).Scan(&p.ID, &p.CreatedAt)

	if err != nil {
		log.Println("Erro insert post:", err)
		h.broker.ReplyError("gateway:replies", env, 500, "Erro ao criar post")
		return
	}

	p.Texto = req.Texto
	p.Author = author
	p.Likes = 0
	p.Replies = []models.Post{}

	h.broker.Reply("gateway:replies", env, p)
	h.broker.Broadcast("gateway:broadcast", "new_post", "social", p)
}

func (h *Handler) comentar(env envelope.Envelope) {
	type commentReq struct {
		ParentID int    `json:"parent_id"`
		Texto    string `json:"texto"`
		Author   string `json:"author"`
	}
	req, _ := envelope.ParseData[commentReq](env)

	if req.ParentID <= 0 {
		h.broker.ReplyError("gateway:replies", env, 400, "parent_id inválido")
		return
	}

	author := req.Author
	if author == "" {
		author = env.Username
	}
	if author == "" {
		author = "Anônimo"
	}

	var reply models.Post
	err := h.db.QueryRow(
		`INSERT INTO posts (texto, author, parent_id, likes)
		 VALUES ($1, $2, $3, 0)
		 RETURNING id, created_at`, req.Texto, author, req.ParentID,
	).Scan(&reply.ID, &reply.CreatedAt)

	if err != nil {
		h.broker.ReplyError("gateway:replies", env, 500, "Erro ao comentar")
		return
	}

	reply.Texto = req.Texto
	reply.Author = author
	reply.ParentID = &req.ParentID
	reply.Likes = 0
	reply.Replies = []models.Post{}

	h.broker.Reply("gateway:replies", env, reply)
	h.broker.Broadcast("gateway:broadcast", "new_comment", "social", reply)
}

func (h *Handler) curtir(env envelope.Envelope) {
	h.toggleLike(env, 1)
}

func (h *Handler) descurtir(env envelope.Envelope) {
	h.toggleLike(env, -1)
}

func (h *Handler) toggleLike(env envelope.Envelope, delta int) {
	type likeReq struct {
		ID int `json:"id"`
	}
	req, _ := envelope.ParseData[likeReq](env)
	if req.ID <= 0 {
		h.broker.ReplyError("gateway:replies", env, 400, "ID inválido")
		return
	}

	var query string
	if delta > 0 {
		query = `UPDATE posts SET likes = likes + 1 WHERE id = $1`
	} else {
		query = `UPDATE posts SET likes = GREATEST(likes - 1, 0) WHERE id = $1`
	}

	result, err := h.db.Exec(query, req.ID)
	if err != nil {
		h.broker.ReplyError("gateway:replies", env, 500, "Erro ao atualizar like")
		return
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		h.broker.ReplyError("gateway:replies", env, 404, "Post não encontrado")
		return
	}

	var newLikes int
	h.db.QueryRow(`SELECT likes FROM posts WHERE id = $1`, req.ID).Scan(&newLikes)

	payload := map[string]interface{}{"post_id": req.ID, "likes": newLikes}
	h.broker.Reply("gateway:replies", env, payload)
	h.broker.Broadcast("gateway:broadcast", "like_updated", "social", payload)
}

func (h *Handler) carregarReplies(parentID int, maxDepth int) []models.Post {
	if maxDepth <= 0 {
		return nil
	}

	rows, err := h.db.Query(
		`SELECT id, texto, author, likes, created_at
		 FROM posts WHERE parent_id = $1
		 ORDER BY created_at ASC LIMIT 20`, parentID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var replies []models.Post
	for rows.Next() {
		var r models.Post
		if err := rows.Scan(&r.ID, &r.Texto, &r.Author, &r.Likes, &r.CreatedAt); err != nil {
			continue
		}
		pid := parentID
		r.ParentID = &pid
		r.Replies = h.carregarReplies(r.ID, maxDepth-1)
		replies = append(replies, r)
	}

	if replies == nil {
		return []models.Post{}
	}
	return replies
}
