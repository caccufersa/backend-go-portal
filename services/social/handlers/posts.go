package handlers

import (
	"database/sql"
	"log"
	"strconv"

	"cacc/services/social/models"

	"github.com/gofiber/fiber/v2"
)

type Handler struct {
	db          *sql.DB
	OnBroadcast func(msgType string, data interface{})
}

func New(db *sql.DB) *Handler {
	return &Handler{db: db}
}

func (h *Handler) ListarFeed(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 50)
	if limit > 100 {
		limit = 100
	}

	query := `
		SELECT id, texto, author, likes, created_at 
		FROM posts 
		WHERE parent_id IS NULL 
		ORDER BY created_at DESC 
		LIMIT $1
	`
	rows, err := h.db.Query(query, limit)
	if err != nil {
		log.Println("Erro query feed:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao buscar feed"})
	}
	defer rows.Close()

	posts := make([]models.Post, 0, limit)
	for rows.Next() {
		var p models.Post
		if err := rows.Scan(&p.ID, &p.Texto, &p.Author, &p.Likes, &p.CreatedAt); err != nil {
			continue
		}

		p.Replies = h.carregarRepliesRapido(p.ID, 3)
		posts = append(posts, p)
	}

	return c.JSON(posts)
}

func (h *Handler) BuscarPerfil(c *fiber.Ctx) error {
	username := c.Params("username")

	query := `
		SELECT id, texto, author, parent_id, likes, created_at 
		FROM posts 
		WHERE author = $1 
		ORDER BY created_at DESC 
		LIMIT 100
	`
	rows, err := h.db.Query(query, username)
	if err != nil {
		log.Println("Erro query perfil:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao buscar perfil"})
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
		p.Replies = h.carregarRepliesRapido(p.ID, 2)
		posts = append(posts, p)
	}

	stats := fiber.Map{
		"username":    username,
		"total_posts": len(posts),
		"total_likes": totalLikes,
		"posts":       posts,
	}

	return c.JSON(stats)
}

func (h *Handler) carregarRepliesRapido(parentID int, maxDepth int) []models.Post {
	if maxDepth <= 0 {
		return nil
	}

	query := `
		SELECT id, texto, author, likes, created_at 
		FROM posts 
		WHERE parent_id = $1 
		ORDER BY created_at ASC 
		LIMIT 20
	`
	rows, err := h.db.Query(query, parentID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	replies := []models.Post{}
	for rows.Next() {
		var r models.Post
		if err := rows.Scan(&r.ID, &r.Texto, &r.Author, &r.Likes, &r.CreatedAt); err != nil {
			continue
		}
		r.ParentID = &parentID
		r.Replies = h.carregarRepliesRapido(r.ID, maxDepth-1)
		replies = append(replies, r)
	}
	return replies
}

func (h *Handler) CriarPost(c *fiber.Ctx) error {
	var p models.Post
	if err := c.BodyParser(&p); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}

	if p.Author == "" {
		p.Author = "Anônimo"
	}

	insertSQL := `
		INSERT INTO posts (texto, author, parent_id, likes) 
		VALUES ($1, $2, $3, 0) 
		RETURNING id, created_at
	`
	err := h.db.QueryRow(insertSQL, p.Texto, p.Author, p.ParentID).Scan(&p.ID, &p.CreatedAt)
	if err != nil {
		log.Println("Erro insert post:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao criar post"})
	}

	p.Likes = 0
	p.Replies = []models.Post{}

	if h.OnBroadcast != nil {
		go h.OnBroadcast("new_post", p)
	}

	return c.Status(201).JSON(p)
}

func (h *Handler) BuscarThread(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "ID inválido"})
	}

	query := `SELECT id, texto, author, parent_id, likes, created_at FROM posts WHERE id = $1`
	var p models.Post
	err = h.db.QueryRow(query, id).Scan(&p.ID, &p.Texto, &p.Author, &p.ParentID, &p.Likes, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return c.Status(404).JSON(fiber.Map{"erro": "Post não encontrado"})
	}
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro no banco"})
	}

	p.Replies = h.carregarRepliesRapido(p.ID, 10)
	return c.JSON(p)
}

func (h *Handler) Comentar(c *fiber.Ctx) error {
	parentID, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "ID inválido"})
	}

	var req struct {
		Texto  string `json:"texto"`
		Author string `json:"author"`
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}

	if req.Author == "" {
		req.Author = "Anônimo"
	}

	var reply models.Post
	insertSQL := `
		INSERT INTO posts (texto, author, parent_id, likes) 
		VALUES ($1, $2, $3, 0) 
		RETURNING id, created_at
	`
	err = h.db.QueryRow(insertSQL, req.Texto, req.Author, parentID).Scan(&reply.ID, &reply.CreatedAt)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao comentar"})
	}

	reply.Texto = req.Texto
	reply.Author = req.Author
	reply.ParentID = &parentID
	reply.Likes = 0
	reply.Replies = []models.Post{}

	if h.OnBroadcast != nil {
		go h.OnBroadcast("new_comment", reply)
	}

	return c.Status(201).JSON(reply)
}

func (h *Handler) Curtir(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "ID inválido"})
	}

	result, err := h.db.Exec(`UPDATE posts SET likes = likes + 1 WHERE id = $1`, id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao curtir"})
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return c.Status(404).JSON(fiber.Map{"erro": "Post não encontrado"})
	}

	var newLikes int
	h.db.QueryRow(`SELECT likes FROM posts WHERE id = $1`, id).Scan(&newLikes)

	if h.OnBroadcast != nil {
		go h.OnBroadcast("like_updated", fiber.Map{"post_id": id, "likes": newLikes})
	}

	return c.JSON(fiber.Map{"status": "ok", "post_id": id, "likes": newLikes})
}

func (h *Handler) Descurtir(c *fiber.Ctx) error {
	id, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "ID inválido"})
	}

	result, err := h.db.Exec(`UPDATE posts SET likes = GREATEST(likes - 1, 0) WHERE id = $1`, id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao descurtir"})
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return c.Status(404).JSON(fiber.Map{"erro": "Post não encontrado"})
	}

	var newLikes int
	h.db.QueryRow(`SELECT likes FROM posts WHERE id = $1`, id).Scan(&newLikes)

	if h.OnBroadcast != nil {
		go h.OnBroadcast("like_updated", fiber.Map{"post_id": id, "likes": newLikes})
	}

	return c.JSON(fiber.Map{"status": "ok", "post_id": id, "likes": newLikes})
}
