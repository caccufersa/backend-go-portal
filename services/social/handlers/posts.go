package handlers

import (
	"database/sql"
	"log"
	"strconv"

	"cacc/services/social/models"

	"github.com/gofiber/fiber/v2"
)

type PostHandler struct {
	DB          *sql.DB
	OnBroadcast func(msgType string, data interface{})
}

func New(db *sql.DB) *PostHandler {
	return &PostHandler{DB: db}
}

func (h *PostHandler) ListarFeed(c *fiber.Ctx) error {
	query := `
		SELECT id, texto, author, parent_id, likes, created_at
		FROM posts
		WHERE parent_id IS NULL
		ORDER BY created_at DESC
		LIMIT 50
	`
	rows, err := h.DB.Query(query)
	if err != nil {
		log.Println("Erro ao listar feed:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro no banco"})
	}
	defer rows.Close()

	posts := []models.Post{}
	for rows.Next() {
		var p models.Post
		if err := rows.Scan(&p.ID, &p.Texto, &p.Author, &p.ParentID, &p.Likes, &p.CreatedAt); err != nil {
			continue
		}
		p.Replies = h.buscarReplies(p.ID)
		posts = append(posts, p)
	}

	return c.JSON(posts)
}

func (h *PostHandler) CriarPost(c *fiber.Ctx) error {
	var p models.Post
	if err := c.BodyParser(&p); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}
	if p.Author == "" {
		p.Author = "Anônimo"
	}

	err := h.DB.QueryRow(
		`INSERT INTO posts (texto, author) VALUES ($1, $2) RETURNING id, created_at`,
		p.Texto, p.Author,
	).Scan(&p.ID, &p.CreatedAt)
	if err != nil {
		log.Println("Erro ao criar post:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao salvar"})
	}

	p.Replies = []models.Post{}
	h.broadcast("new_post", p)
	return c.Status(201).JSON(p)
}

func (h *PostHandler) Comentar(c *fiber.Ctx) error {
	parentID, err := strconv.Atoi(c.Params("id"))
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "ID inválido"})
	}

	var p models.Post
	if err := c.BodyParser(&p); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}
	if p.Author == "" {
		p.Author = "Anônimo"
	}

	err = h.DB.QueryRow(
		`INSERT INTO posts (texto, author, parent_id) VALUES ($1, $2, $3) RETURNING id, created_at`,
		p.Texto, p.Author, parentID,
	).Scan(&p.ID, &p.CreatedAt)
	if err != nil {
		log.Println("Erro ao comentar:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao salvar"})
	}

	p.ParentID = &parentID
	p.Replies = []models.Post{}
	h.broadcast("new_comment", p)
	return c.Status(201).JSON(p)
}

func (h *PostHandler) Curtir(c *fiber.Ctx) error {
	id := c.Params("id")
	var p models.Post
	err := h.DB.QueryRow(
		`UPDATE posts SET likes = likes + 1 WHERE id = $1
		 RETURNING id, texto, author, parent_id, likes, created_at`, id,
	).Scan(&p.ID, &p.Texto, &p.Author, &p.ParentID, &p.Likes, &p.CreatedAt)
	if err != nil {
		log.Println("Erro ao curtir:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao curtir"})
	}

	h.broadcast("like_updated", p)
	return c.JSON(p)
}

func (h *PostHandler) Descurtir(c *fiber.Ctx) error {
	id := c.Params("id")
	var p models.Post
	err := h.DB.QueryRow(
		`UPDATE posts SET likes = GREATEST(likes - 1, 0) WHERE id = $1
		 RETURNING id, texto, author, parent_id, likes, created_at`, id,
	).Scan(&p.ID, &p.Texto, &p.Author, &p.ParentID, &p.Likes, &p.CreatedAt)
	if err != nil {
		log.Println("Erro ao descurtir:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao descurtir"})
	}

	h.broadcast("like_updated", p)
	return c.JSON(p)
}

func (h *PostHandler) BuscarThread(c *fiber.Ctx) error {
	id := c.Params("id")
	var p models.Post
	err := h.DB.QueryRow(
		`SELECT id, texto, author, parent_id, likes, created_at FROM posts WHERE id = $1`, id,
	).Scan(&p.ID, &p.Texto, &p.Author, &p.ParentID, &p.Likes, &p.CreatedAt)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"erro": "Post não encontrado"})
	}

	p.Replies = h.buscarReplies(p.ID)
	return c.JSON(p)
}

func (h *PostHandler) buscarReplies(parentID int) []models.Post {
	rows, err := h.DB.Query(
		`SELECT id, texto, author, parent_id, likes, created_at
		 FROM posts WHERE parent_id = $1 ORDER BY created_at ASC`, parentID,
	)
	if err != nil {
		return []models.Post{}
	}
	defer rows.Close()

	replies := []models.Post{}
	for rows.Next() {
		var r models.Post
		if err := rows.Scan(&r.ID, &r.Texto, &r.Author, &r.ParentID, &r.Likes, &r.CreatedAt); err != nil {
			continue
		}
		r.Replies = h.buscarReplies(r.ID)
		replies = append(replies, r)
	}
	return replies
}

func (h *PostHandler) broadcast(msgType string, data interface{}) {
	if h.OnBroadcast != nil {
		go h.OnBroadcast(msgType, data)
	}
}
