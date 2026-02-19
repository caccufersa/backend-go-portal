package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"cacc/pkg/cache"
	"cacc/pkg/envelope"
	"cacc/pkg/hub"
	"cacc/pkg/models"
)

// ──────────────────────────────────────────────
// SocialHandler — optimized for serverless PG
// ──────────────────────────────────────────────

type SocialHandler struct {
	db    *sql.DB
	hub   *hub.Hub
	redis *cache.Redis

	// Prepared statements for hot paths
	stmtFeed         *sql.Stmt
	stmtThread       *sql.Stmt
	stmtReplies      *sql.Stmt
	stmtProfile      *sql.Stmt
	stmtProfileStats *sql.Stmt
	stmtInsertPost   *sql.Stmt
	stmtInsertReply  *sql.Stmt

	// Like system
	stmtLikeInsert *sql.Stmt // Tenta inserir na post_likes
	stmtLikeDelete *sql.Stmt // Tenta deletar da post_likes
	stmtLikeInc    *sql.Stmt // Incrementa contador no posts
	stmtLikeDec    *sql.Stmt // Decrementa contador no posts
	stmtGetLikes   *sql.Stmt // Busca likes atuais

	stmtDeletePost *sql.Stmt
}

func NewSocial(db *sql.DB, h *hub.Hub, r *cache.Redis) *SocialHandler {
	s := &SocialHandler{db: db, hub: h, redis: r}
	s.prepareStatements()
	return s
}

func (s *SocialHandler) prepareStatements() {
	var err error

	// FEED: agora verifica se o usuário (param $3) curtiu
	// Usamos EXISTS ou COALESCE na junção. Assumindo $3 = user_id do request
	s.stmtFeed, err = s.db.Prepare(`
		SELECT p.id, p.texto, p.author, COALESCE(p.user_id, 0), p.likes, p.reply_count, p.created_at,
		       EXISTS(SELECT 1 FROM post_likes pl WHERE pl.post_id = p.id AND pl.user_id = $3) AS liked
		FROM posts p
		WHERE p.parent_id IS NULL
		ORDER BY p.created_at DESC
		LIMIT $1 OFFSET $2
	`)
	if err != nil {
		log.Fatalf("[SOCIAL] FATAL: prepare feed: %v", err)
	}

	s.stmtThread, err = s.db.Prepare(`
		SELECT p.id, p.texto, p.author, COALESCE(p.user_id, 0), p.parent_id, p.likes, p.reply_count, p.created_at,
		       EXISTS(SELECT 1 FROM post_likes pl WHERE pl.post_id = p.id AND pl.user_id = $2) AS liked
		FROM posts p WHERE p.id = $1
	`)
	if err != nil {
		log.Fatalf("[SOCIAL] FATAL: prepare thread: %v", err)
	}

	s.stmtReplies, err = s.db.Prepare(`
		SELECT p.id, p.texto, p.author, COALESCE(p.user_id, 0), p.likes, p.reply_count, p.created_at,
		       EXISTS(SELECT 1 FROM post_likes pl WHERE pl.post_id = p.id AND pl.user_id = $2) AS liked
		FROM posts p WHERE p.parent_id = $1
		ORDER BY p.created_at ASC
		LIMIT 50
	`)
	if err != nil {
		log.Fatalf("[SOCIAL] FATAL: prepare replies: %v", err)
	}

	s.stmtProfile, err = s.db.Prepare(`
		SELECT p.id, p.texto, p.author, COALESCE(p.user_id, 0), p.parent_id, p.likes, p.reply_count, p.created_at,
		       EXISTS(SELECT 1 FROM post_likes pl WHERE pl.post_id = p.id AND pl.user_id = $2) AS liked
		FROM posts p WHERE p.user_id = $1
		ORDER BY p.created_at DESC
		LIMIT 100
	`)
	if err != nil {
		log.Fatalf("[SOCIAL] FATAL: prepare profile: %v", err)
	}

	s.stmtProfileStats, err = s.db.Prepare(`
		SELECT COUNT(*), COALESCE(SUM(likes), 0)
		FROM posts WHERE user_id = $1
	`)
	if err != nil {
		log.Fatalf("[SOCIAL] FATAL: prepare profile stats: %v", err)
	}

	s.stmtInsertPost, err = s.db.Prepare(`
		INSERT INTO posts (texto, author, user_id, parent_id, likes, reply_count)
		VALUES ($1, $2, $3, NULL, 0, 0)
		RETURNING id, created_at
	`)
	if err != nil {
		log.Fatalf("[SOCIAL] FATAL: prepare insert post: %v", err)
	}

	s.stmtInsertReply, err = s.db.Prepare(`
		WITH new_reply AS (
			INSERT INTO posts (texto, author, user_id, parent_id, likes, reply_count)
			VALUES ($1, $2, $3, $4, 0, 0)
			RETURNING id, created_at
		)
		SELECT nr.id, nr.created_at FROM new_reply nr
	`)
	if err != nil {
		log.Fatalf("[SOCIAL] FATAL: prepare insert reply: %v", err)
	}

	// LIKE SYSTEM
	// Tenta inserir like. Retorna 1 se inseriu, 0 se duplicado.
	s.stmtLikeInsert, err = s.db.Prepare(`
		INSERT INTO post_likes (user_id, post_id) VALUES ($1, $2)
		ON CONFLICT (user_id, post_id) DO NOTHING
		RETURNING 1
	`)
	if err != nil {
		log.Fatalf("[SOCIAL] FATAL: prepare like insert: %v", err)
	}

	s.stmtLikeInc, err = s.db.Prepare(`
		UPDATE posts SET likes = likes + 1 WHERE id = $1
		RETURNING likes
	`)
	if err != nil {
		log.Fatalf("[SOCIAL] FATAL: prepare like inc: %v", err)
	}

	// UNLIKE
	// Tenta deletar like. Retorna 1 se deletou, 0 se não existia.
	s.stmtLikeDelete, err = s.db.Prepare(`
		DELETE FROM post_likes WHERE user_id = $1 AND post_id = $2
		RETURNING 1
	`)
	if err != nil {
		log.Fatalf("[SOCIAL] FATAL: prepare like delete: %v", err)
	}

	s.stmtLikeDec, err = s.db.Prepare(`
		UPDATE posts SET likes = GREATEST(likes - 1, 0) WHERE id = $1
		RETURNING likes
	`)
	if err != nil {
		log.Fatalf("[SOCIAL] FATAL: prepare like dec: %v", err)
	}

	s.stmtGetLikes, err = s.db.Prepare(`SELECT likes FROM posts WHERE id = $1`)
	if err != nil {
		log.Fatalf("[SOCIAL] FATAL: prepare get likes: %v", err)
	}

	s.stmtDeletePost, err = s.db.Prepare(`
		DELETE FROM posts WHERE id = $1 AND user_id = $2
		RETURNING id
	`)
	if err != nil {
		log.Fatalf("[SOCIAL] FATAL: prepare delete: %v", err)
	}
}

func (s *SocialHandler) RegisterActions() {
	s.hub.On("social.feed", s.listarFeed)
	s.hub.On("social.thread", s.buscarThread)
	s.hub.On("social.profile", s.buscarPerfil)
	s.hub.On("social.post.create", s.criarPost)
	s.hub.On("social.post.comment", s.comentar)
	s.hub.On("social.post.like", s.curtir)
	s.hub.On("social.post.unlike", s.descurtir)
	s.hub.On("social.post.delete", s.deletar)
}

// ──────────────────────────────────────────────
// FEED — paginated, cached, no N+1
// ──────────────────────────────────────────────

func (s *SocialHandler) listarFeed(env envelope.Envelope) {
	type feedReq struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}
	req, _ := envelope.ParseData[feedReq](env)
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 30
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	// Cache key includes userID because 'liked' status is personalized
	cacheKey := fmt.Sprintf("social:feed:%d:%d:lid%d", req.Limit, req.Offset, env.UserID)
	var cached []models.Post
	if s.redis.Get(cacheKey, &cached) {
		s.hub.Reply(env, cached)
		return
	}

	rows, err := s.stmtFeed.Query(req.Limit, req.Offset, env.UserID)
	if err != nil {
		s.hub.ReplyError(env, 500, "Erro ao buscar feed")
		return
	}
	defer rows.Close()

	posts := make([]models.Post, 0, req.Limit)
	var postIDs []int
	for rows.Next() {
		var p models.Post
		if err := rows.Scan(&p.ID, &p.Texto, &p.Author, &p.UserID, &p.Likes, &p.ReplyCount, &p.CreatedAt, &p.Liked); err != nil {
			continue
		}
		p.Replies = []models.Post{}
		postIDs = append(postIDs, p.ID)
		posts = append(posts, p)
	}

	// Batch load first-level replies for all posts (no N+1)
	if len(postIDs) > 0 {
		repliesMap := s.batchLoadReplies(postIDs, env.UserID)
		for i := range posts {
			if replies, ok := repliesMap[posts[i].ID]; ok {
				posts[i].Replies = replies
			}
		}
	}

	s.redis.Set(cacheKey, posts, 15*time.Second)
	s.hub.Reply(env, posts)
}

// batchLoadReplies loads first-level replies for multiple parent IDs in ONE query
func (s *SocialHandler) batchLoadReplies(parentIDs []int, userID int) map[int][]models.Post {
	result := make(map[int][]models.Post, len(parentIDs))

	placeholders := make([]string, len(parentIDs))
	args := make([]interface{}, len(parentIDs))
	for i, id := range parentIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT p.id, p.texto, p.author, COALESCE(p.user_id, 0), p.parent_id, p.likes, p.reply_count, p.created_at,
		       EXISTS(SELECT 1 FROM post_likes pl WHERE pl.post_id = p.id AND pl.user_id = $1) AS liked
		FROM posts p
		WHERE p.parent_id IN (%s)
		ORDER BY p.created_at ASC
	`, strings.Join(placeholders, ","))

	// Re-map args: userID ($1) comes first, then parentIDs
	newArgs := make([]interface{}, len(args)+1)
	newArgs[0] = userID
	for i, v := range args {
		newArgs[i+1] = v
	}

	rows, err := s.db.Query(query, newArgs...)
	if err != nil {
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var r models.Post
		var parentID int
		if err := rows.Scan(&r.ID, &r.Texto, &r.Author, &r.UserID, &parentID, &r.Likes, &r.ReplyCount, &r.CreatedAt, &r.Liked); err != nil {
			continue
		}
		r.ParentID = &parentID
		r.Replies = []models.Post{}
		result[parentID] = append(result[parentID], r)
	}

	return result
}

// ──────────────────────────────────────────────
// THREAD — single post + recursive replies
// ──────────────────────────────────────────────

func (s *SocialHandler) buscarThread(env envelope.Envelope) {
	type threadReq struct {
		ID int `json:"id"`
	}
	req, _ := envelope.ParseData[threadReq](env)
	if req.ID <= 0 {
		s.hub.ReplyError(env, 400, "ID inválido")
		return
	}

	cacheKey := fmt.Sprintf("social:thread:%d:lid%d", req.ID, env.UserID)
	var cached models.Post
	if s.redis.Get(cacheKey, &cached) {
		s.hub.Reply(env, cached)
		return
	}

	var p models.Post
	err := s.stmtThread.QueryRow(req.ID, env.UserID).Scan(
		&p.ID, &p.Texto, &p.Author, &p.UserID, &p.ParentID, &p.Likes, &p.ReplyCount, &p.CreatedAt, &p.Liked,
	)
	if err == sql.ErrNoRows {
		s.hub.ReplyError(env, 404, "Post não encontrado")
		return
	}
	if err != nil {
		s.hub.ReplyError(env, 500, "Erro no banco")
		return
	}

	p.Replies = s.loadRepliesRecursive(p.ID, 5, env.UserID)
	s.redis.Set(cacheKey, p, 30*time.Second)
	s.hub.Reply(env, p)
}

func (s *SocialHandler) loadRepliesRecursive(parentID int, maxDepth int, userID int) []models.Post {
	if maxDepth <= 0 {
		return []models.Post{}
	}

	rows, err := s.stmtReplies.Query(parentID, userID)
	if err != nil {
		return []models.Post{}
	}
	defer rows.Close()

	replies := []models.Post{}
	for rows.Next() {
		var r models.Post
		if err := rows.Scan(&r.ID, &r.Texto, &r.Author, &r.UserID, &r.Likes, &r.ReplyCount, &r.CreatedAt, &r.Liked); err != nil {
			continue
		}
		pid := parentID
		r.ParentID = &pid
		if r.ReplyCount > 0 {
			r.Replies = s.loadRepliesRecursive(r.ID, maxDepth-1, userID)
		} else {
			r.Replies = []models.Post{}
		}
		replies = append(replies, r)
	}

	return replies
}

// ──────────────────────────────────────────────
// PROFILE — user posts + stats
// ──────────────────────────────────────────────

func (s *SocialHandler) buscarPerfil(env envelope.Envelope) {
	type profileReq struct {
		Username string `json:"username"`
		UserID   int    `json:"user_id"`
	}
	req, _ := envelope.ParseData[profileReq](env)

	// Resolve user_id from username if needed
	userID := req.UserID
	username := req.Username

	if userID <= 0 && username == "" {
		// Use the requesting user's own profile
		userID = env.UserID
		username = env.Username
	}

	if userID <= 0 && username != "" {
		// Lookup user_id by username
		err := s.db.QueryRow(`SELECT id FROM users WHERE username = $1`, strings.ToLower(username)).Scan(&userID)
		if err != nil {
			s.hub.ReplyError(env, 404, "Usuário não encontrado")
			return
		}
	}

	if userID <= 0 {
		s.hub.ReplyError(env, 400, "Username ou user_id obrigatório")
		return
	}

	cacheKey := fmt.Sprintf("social:profile:%d:lid%d", userID, env.UserID)
	var cached models.Profile
	if s.redis.Get(cacheKey, &cached) {
		s.hub.Reply(env, cached)
		return
	}

	// Get stats in parallel with posts
	var totalPosts, totalLikes int
	if err := s.stmtProfileStats.QueryRow(userID).Scan(&totalPosts, &totalLikes); err != nil {
		totalPosts = 0
		totalLikes = 0
	}

	rows, err := s.stmtProfile.Query(userID, env.UserID)
	if err != nil {
		s.hub.ReplyError(env, 500, "Erro ao buscar perfil")
		return
	}
	defer rows.Close()

	posts := []models.Post{}
	for rows.Next() {
		var p models.Post
		if err := rows.Scan(&p.ID, &p.Texto, &p.Author, &p.UserID, &p.ParentID, &p.Likes, &p.ReplyCount, &p.CreatedAt, &p.Liked); err != nil {
			continue
		}
		p.Replies = []models.Post{}
		posts = append(posts, p)
	}

	// Batch load replies for all profile posts
	if len(posts) > 0 {
		ids := make([]int, len(posts))
		for i, p := range posts {
			ids[i] = p.ID
		}
		repliesMap := s.batchLoadReplies(ids, env.UserID)
		for i := range posts {
			if replies, ok := repliesMap[posts[i].ID]; ok {
				posts[i].Replies = replies
			}
		}
	}

	if username == "" {
		s.db.QueryRow(`SELECT username FROM users WHERE id = $1`, userID).Scan(&username)
	}

	profile := models.Profile{
		Username:   username,
		TotalPosts: totalPosts,
		TotalLikes: totalLikes,
		Posts:      posts,
	}

	s.redis.Set(cacheKey, profile, 30*time.Second)
	s.hub.Reply(env, profile)
}

// ──────────────────────────────────────────────
// CREATE POST — requires auth
// ──────────────────────────────────────────────

func (s *SocialHandler) criarPost(env envelope.Envelope) {
	if env.UserID <= 0 {
		s.hub.ReplyError(env, 401, "Autenticação necessária para criar post")
		return
	}

	type createReq struct {
		Texto string `json:"texto"`
	}
	req, _ := envelope.ParseData[createReq](env)

	texto := strings.TrimSpace(req.Texto)
	if texto == "" {
		s.hub.ReplyError(env, 400, "Texto não pode ser vazio")
		return
	}
	if len(texto) > 5000 {
		s.hub.ReplyError(env, 400, "Texto muito longo (max 5000 chars)")
		return
	}

	var p models.Post
	err := s.stmtInsertPost.QueryRow(texto, env.Username, env.UserID).Scan(&p.ID, &p.CreatedAt)
	if err != nil {
		log.Printf("[SOCIAL] Erro criar post: %v", err)
		s.hub.ReplyError(env, 500, "Erro ao criar post")
		return
	}

	p.Texto = texto
	p.Author = env.Username
	p.UserID = env.UserID
	p.Likes = 0
	p.ReplyCount = 0
	p.Replies = []models.Post{}

	s.invalidateFeedCache()
	s.redis.Del(fmt.Sprintf("social:profile:%d", env.UserID))

	log.Printf("[SOCIAL] Post criado: id=%d author=%s user_id=%d", p.ID, p.Author, p.UserID)
	s.hub.Reply(env, p)
	s.hub.Broadcast("new_post", "social", p)
}

// ──────────────────────────────────────────────
// COMMENT — requires auth
// ──────────────────────────────────────────────

func (s *SocialHandler) comentar(env envelope.Envelope) {
	if env.UserID <= 0 {
		s.hub.ReplyError(env, 401, "Autenticação necessária para comentar")
		return
	}

	type commentReq struct {
		ParentID int    `json:"parent_id"`
		Texto    string `json:"texto"`
	}
	req, _ := envelope.ParseData[commentReq](env)

	if req.ParentID <= 0 {
		s.hub.ReplyError(env, 400, "parent_id inválido")
		return
	}

	texto := strings.TrimSpace(req.Texto)
	if texto == "" {
		s.hub.ReplyError(env, 400, "Texto não pode ser vazio")
		return
	}
	if len(texto) > 5000 {
		s.hub.ReplyError(env, 400, "Texto muito longo (max 5000 chars)")
		return
	}

	var reply models.Post
	err := s.stmtInsertReply.QueryRow(texto, env.Username, env.UserID, req.ParentID).Scan(&reply.ID, &reply.CreatedAt)
	if err != nil {
		log.Printf("[SOCIAL] Erro comentar: %v", err)
		s.hub.ReplyError(env, 500, "Erro ao comentar")
		return
	}

	// Increment reply_count on parent
	s.db.Exec(`UPDATE posts SET reply_count = reply_count + 1 WHERE id = $1`, req.ParentID)

	reply.Texto = texto
	reply.Author = env.Username
	reply.UserID = env.UserID
	reply.ParentID = &req.ParentID
	reply.Likes = 0
	reply.ReplyCount = 0
	reply.Replies = []models.Post{}

	s.redis.Del(fmt.Sprintf("social:thread:%d", req.ParentID))
	s.invalidateFeedCache()
	s.redis.Del(fmt.Sprintf("social:profile:%d", env.UserID))

	log.Printf("[SOCIAL] Comentário: id=%d parent=%d author=%s", reply.ID, req.ParentID, reply.Author)
	s.hub.Reply(env, reply)
	s.hub.Broadcast("new_comment", "social", reply)
}

// ──────────────────────────────────────────────
// LIKE / UNLIKE
// ──────────────────────────────────────────────

func (s *SocialHandler) curtir(env envelope.Envelope) {
	s.toggleLike(env, true)
}

func (s *SocialHandler) descurtir(env envelope.Envelope) {
	s.toggleLike(env, false)
}

func (s *SocialHandler) toggleLike(env envelope.Envelope, isLike bool) {
	type likeReq struct {
		ID int `json:"id"`
	}
	req, _ := envelope.ParseData[likeReq](env)
	if req.ID <= 0 {
		s.hub.ReplyError(env, 400, "ID inválido")
		return
	}

	if env.UserID <= 0 {
		s.hub.ReplyError(env, 401, "Login necessário")
		return
	}

	var success bool
	var err error
	var dummy int

	// 1. Tentar inserir/remover da tabela de likes
	if isLike {
		err = s.stmtLikeInsert.QueryRow(env.UserID, req.ID).Scan(&dummy)
	} else {
		err = s.stmtLikeDelete.QueryRow(env.UserID, req.ID).Scan(&dummy)
	}

	// 2. Se erro for NoRows, significa que já estava likeado (no insert) ou não estava likeado (no delete)
	// Nesse caso, não fazemos nada no contador.
	if err == sql.ErrNoRows {
		success = false
	} else if err != nil {
		log.Printf("[SOCIAL] Erro like/unlike: %v", err)
		s.hub.ReplyError(env, 500, "Erro interno")
		return
	} else {
		success = true
	}

	// 3. Atualizar contador apenas se houve mudança real
	var newLikes int
	if success {
		var stmtCount *sql.Stmt
		if isLike {
			stmtCount = s.stmtLikeInc
		} else {
			stmtCount = s.stmtLikeDec
		}

		err = stmtCount.QueryRow(req.ID).Scan(&newLikes)
		if err != nil {
			// Se o post foi deletado entretanto...
			s.hub.ReplyError(env, 404, "Post não encontrado")
			return
		}
	} else {
		// Se nada mudou, precisamos buscar o valor atual para retornar ao front
		err = s.stmtGetLikes.QueryRow(req.ID).Scan(&newLikes)
		if err != nil {
			newLikes = 0 // Post sumiu?
		}
	}

	payload := map[string]interface{}{"post_id": req.ID, "likes": newLikes}
	s.redis.Del(fmt.Sprintf("social:thread:%d", req.ID))
	s.invalidateFeedCache()

	s.hub.Reply(env, payload)
	s.hub.Broadcast("like_updated", "social", payload)
}

// ──────────────────────────────────────────────
// DELETE — only the post author can delete
// ──────────────────────────────────────────────

func (s *SocialHandler) deletar(env envelope.Envelope) {
	if env.UserID <= 0 {
		s.hub.ReplyError(env, 401, "Autenticação necessária")
		return
	}

	type deleteReq struct {
		ID int `json:"id"`
	}
	req, _ := envelope.ParseData[deleteReq](env)
	if req.ID <= 0 {
		s.hub.ReplyError(env, 400, "ID inválido")
		return
	}

	var deletedID int
	err := s.stmtDeletePost.QueryRow(req.ID, env.UserID).Scan(&deletedID)
	if err == sql.ErrNoRows {
		s.hub.ReplyError(env, 404, "Post não encontrado ou sem permissão")
		return
	}
	if err != nil {
		s.hub.ReplyError(env, 500, "Erro ao deletar")
		return
	}

	s.invalidateFeedCache()
	s.redis.Del(fmt.Sprintf("social:thread:%d", req.ID))
	s.redis.Del(fmt.Sprintf("social:profile:%d", env.UserID))

	payload := map[string]interface{}{"id": req.ID, "status": "deleted"}
	s.hub.Reply(env, payload)
	s.hub.Broadcast("post_deleted", "social", payload)
}

// ──────────────────────────────────────────────
// Cache helpers
// ──────────────────────────────────────────────

func (s *SocialHandler) invalidateFeedCache() {
	s.redis.DelPattern("social:feed:*")
}
