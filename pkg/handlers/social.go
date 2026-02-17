package handlers

import (
	"database/sql"
	"fmt"

	"cacc/pkg/cache"
	"cacc/pkg/envelope"
	"cacc/pkg/hub"
	"cacc/pkg/models"
)

type SocialHandler struct {
	db    *sql.DB
	hub   *hub.Hub
	redis *cache.Redis
}

func NewSocial(db *sql.DB, h *hub.Hub, r *cache.Redis) *SocialHandler {
	return &SocialHandler{db: db, hub: h, redis: r}
}

func (s *SocialHandler) RegisterActions() {
	s.hub.On("social.feed", s.listarFeed)
	s.hub.On("social.thread", s.buscarThread)
	s.hub.On("social.profile", s.buscarPerfil)
	s.hub.On("social.post.create", s.criarPost)
	s.hub.On("social.post.comment", s.comentar)
	s.hub.On("social.post.like", s.curtir)
	s.hub.On("social.post.unlike", s.descurtir)
}

func (s *SocialHandler) listarFeed(env envelope.Envelope) {
	type feedReq struct {
		Limit int `json:"limit"`
	}
	req, _ := envelope.ParseData[feedReq](env)
	limit := req.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	cacheKey := fmt.Sprintf("feed:%d", limit)
	var cached []models.Post
	if s.redis.Get(cacheKey, &cached) {
		s.hub.Reply(env, cached)
		return
	}

	rows, err := s.db.Query(
		`SELECT id, texto, author, likes, created_at
		 FROM posts WHERE parent_id IS NULL
		 ORDER BY created_at DESC LIMIT $1`, limit,
	)
	if err != nil {
		s.hub.ReplyError(env, 500, "Erro ao buscar feed")
		return
	}
	defer rows.Close()

	posts := make([]models.Post, 0, limit)
	for rows.Next() {
		var p models.Post
		if err := rows.Scan(&p.ID, &p.Texto, &p.Author, &p.Likes, &p.CreatedAt); err != nil {
			continue
		}
		p.Replies = s.carregarReplies(p.ID, 3)
		posts = append(posts, p)
	}

	s.redis.Set(cacheKey, posts, 15e9)
	s.hub.Reply(env, posts)
}

func (s *SocialHandler) buscarThread(env envelope.Envelope) {
	type threadReq struct {
		ID int `json:"id"`
	}
	req, _ := envelope.ParseData[threadReq](env)
	if req.ID <= 0 {
		s.hub.ReplyError(env, 400, "ID inválido")
		return
	}

	cacheKey := fmt.Sprintf("thread:%d", req.ID)
	var cached models.Post
	if s.redis.Get(cacheKey, &cached) {
		s.hub.Reply(env, cached)
		return
	}

	var p models.Post
	err := s.db.QueryRow(
		`SELECT id, texto, author, parent_id, likes, created_at FROM posts WHERE id = $1`, req.ID,
	).Scan(&p.ID, &p.Texto, &p.Author, &p.ParentID, &p.Likes, &p.CreatedAt)

	if err == sql.ErrNoRows {
		s.hub.ReplyError(env, 404, "Post não encontrado")
		return
	}
	if err != nil {
		s.hub.ReplyError(env, 500, "Erro no banco")
		return
	}

	p.Replies = s.carregarReplies(p.ID, 10)
	s.redis.Set(cacheKey, p, 30e9)
	s.hub.Reply(env, p)
}

func (s *SocialHandler) buscarPerfil(env envelope.Envelope) {
	type profileReq struct {
		Username string `json:"username"`
	}
	req, _ := envelope.ParseData[profileReq](env)
	if req.Username == "" {
		s.hub.ReplyError(env, 400, "Username obrigatório")
		return
	}

	rows, err := s.db.Query(
		`SELECT id, texto, author, parent_id, likes, created_at
		 FROM posts WHERE author = $1
		 ORDER BY created_at DESC LIMIT 100`, req.Username,
	)
	if err != nil {
		s.hub.ReplyError(env, 500, "Erro ao buscar perfil")
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
		p.Replies = s.carregarReplies(p.ID, 2)
		posts = append(posts, p)
	}

	s.hub.Reply(env, map[string]interface{}{
		"username": req.Username, "total_posts": len(posts),
		"total_likes": totalLikes, "posts": posts,
	})
}

func (s *SocialHandler) criarPost(env envelope.Envelope) {
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
	err := s.db.QueryRow(
		`INSERT INTO posts (texto, author, parent_id, likes)
		 VALUES ($1, $2, NULL, 0)
		 RETURNING id, created_at`, req.Texto, author,
	).Scan(&p.ID, &p.CreatedAt)

	if err != nil {
		s.hub.ReplyError(env, 500, "Erro ao criar post")
		return
	}

	p.Texto = req.Texto
	p.Author = author
	p.Likes = 0
	p.Replies = []models.Post{}

	s.redis.DelPattern("feed:*")
	s.hub.Reply(env, p)
	s.hub.Broadcast("new_post", "social", p)
}

func (s *SocialHandler) comentar(env envelope.Envelope) {
	type commentReq struct {
		ParentID int    `json:"parent_id"`
		Texto    string `json:"texto"`
		Author   string `json:"author"`
	}
	req, _ := envelope.ParseData[commentReq](env)

	if req.ParentID <= 0 {
		s.hub.ReplyError(env, 400, "parent_id inválido")
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
	err := s.db.QueryRow(
		`INSERT INTO posts (texto, author, parent_id, likes)
		 VALUES ($1, $2, $3, 0)
		 RETURNING id, created_at`, req.Texto, author, req.ParentID,
	).Scan(&reply.ID, &reply.CreatedAt)

	if err != nil {
		s.hub.ReplyError(env, 500, "Erro ao comentar")
		return
	}

	reply.Texto = req.Texto
	reply.Author = author
	reply.ParentID = &req.ParentID
	reply.Likes = 0
	reply.Replies = []models.Post{}

	s.redis.Del(fmt.Sprintf("thread:%d", req.ParentID))
	s.redis.DelPattern("feed:*")
	s.hub.Reply(env, reply)
	s.hub.Broadcast("new_comment", "social", reply)
}

func (s *SocialHandler) curtir(env envelope.Envelope) {
	s.toggleLike(env, 1)
}

func (s *SocialHandler) descurtir(env envelope.Envelope) {
	s.toggleLike(env, -1)
}

func (s *SocialHandler) toggleLike(env envelope.Envelope, delta int) {
	type likeReq struct {
		ID int `json:"id"`
	}
	req, _ := envelope.ParseData[likeReq](env)
	if req.ID <= 0 {
		s.hub.ReplyError(env, 400, "ID inválido")
		return
	}

	var query string
	if delta > 0 {
		query = `UPDATE posts SET likes = likes + 1 WHERE id = $1`
	} else {
		query = `UPDATE posts SET likes = GREATEST(likes - 1, 0) WHERE id = $1`
	}

	result, err := s.db.Exec(query, req.ID)
	if err != nil {
		s.hub.ReplyError(env, 500, "Erro ao atualizar like")
		return
	}

	rowsAff, _ := result.RowsAffected()
	if rowsAff == 0 {
		s.hub.ReplyError(env, 404, "Post não encontrado")
		return
	}

	var newLikes int
	s.db.QueryRow(`SELECT likes FROM posts WHERE id = $1`, req.ID).Scan(&newLikes)

	payload := map[string]interface{}{"post_id": req.ID, "likes": newLikes}
	s.redis.Del(fmt.Sprintf("thread:%d", req.ID))
	s.redis.DelPattern("feed:*")
	s.hub.Reply(env, payload)
	s.hub.Broadcast("like_updated", "social", payload)
}

func (s *SocialHandler) carregarReplies(parentID int, maxDepth int) []models.Post {
	if maxDepth <= 0 {
		return nil
	}

	rows, err := s.db.Query(
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
		r.Replies = s.carregarReplies(r.ID, maxDepth-1)
		replies = append(replies, r)
	}

	if replies == nil {
		return []models.Post{}
	}
	return replies
}
