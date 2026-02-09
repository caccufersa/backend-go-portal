package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"

	"cacc/services/auth/models"

	"github.com/gofiber/fiber/v2"
)

type AuthHandler struct {
	DB *sql.DB
}

func New(db *sql.DB) *AuthHandler {
	return &AuthHandler{DB: db}
}

func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req models.LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "JSON inválido"})
	}

	if len(req.Username) < 3 || len(req.Password) != 6 {
		return c.Status(400).JSON(fiber.Map{"erro": "Username mín 3 chars, senha deve ter 6 caracteres"})
	}

	if err := ValidatePassword(req.Password); err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": err.Error()})
	}

	hashed := hashPassword(req.Password)

	var user models.User
	err := h.DB.QueryRow(
		`SELECT id, username, password, created_at FROM users WHERE username = $1`,
		req.Username,
	).Scan(&user.ID, &user.Username, &user.Password, &user.CreatedAt)

	if err == sql.ErrNoRows {
		err = h.DB.QueryRow(
			`INSERT INTO users (username, password) VALUES ($1, $2) RETURNING id, username, created_at`,
			req.Username, hashed,
		).Scan(&user.ID, &user.Username, &user.CreatedAt)
		if err != nil {
			log.Println("Erro ao criar user:", err)
			return c.Status(500).JSON(fiber.Map{"erro": "Erro ao criar conta"})
		}

		token := generateToken()
		h.saveToken(user.ID, token)
		return c.Status(201).JSON(models.LoginResponse{Token: token, Username: user.Username})
	}

	if err != nil {
		log.Println("Erro ao buscar user:", err)
		return c.Status(500).JSON(fiber.Map{"erro": "Erro interno"})
	}

	if user.Password != hashed {
		return c.Status(401).JSON(fiber.Map{"erro": "Senha incorreta"})
	}

	token := generateToken()
	h.saveToken(user.ID, token)
	return c.JSON(models.LoginResponse{Token: token, Username: user.Username})
}

func (h *AuthHandler) ValidarToken(c *fiber.Ctx) error {
	token := c.Get("Authorization")
	if token == "" {
		return c.Status(401).JSON(fiber.Map{"erro": "Token não informado"})
	}

	var username string
	err := h.DB.QueryRow(
		`SELECT u.username FROM tokens t JOIN users u ON u.id = t.user_id WHERE t.token = $1`,
		token,
	).Scan(&username)
	if err != nil {
		return c.Status(401).JSON(fiber.Map{"erro": "Token inválido"})
	}

	return c.JSON(fiber.Map{"username": username})
}

func (h *AuthHandler) saveToken(userID int, token string) {
	h.DB.Exec(`DELETE FROM tokens WHERE user_id = $1`, userID)
	h.DB.Exec(`INSERT INTO tokens (user_id, token) VALUES ($1, $2)`, userID, token)
}

func hashPassword(pw string) string {
	h := sha256.Sum256([]byte(pw))
	return hex.EncodeToString(h[:])
}

func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}


func ValidatePassword(pw string) error {
	if len(pw) != 6 {
		return fmt.Errorf("senha deve ter 6 caracteres")
	}
	return nil
}
