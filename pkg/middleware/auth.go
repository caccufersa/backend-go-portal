package middleware

import (
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

func AuthMiddleware(c *fiber.Ctx) error {
	tokenStr := ""
	auth := c.Get("Authorization")
	if auth != "" {
		parts := strings.Split(auth, " ")
		if len(parts) == 2 && parts[0] == "Bearer" {
			tokenStr = parts[1]
		}
	}

	if tokenStr == "" {
		tokenStr = c.Cookies("access_token")
	}

	if tokenStr == "" {
		return c.Status(401).JSON(fiber.Map{"erro": "Token não informado"})
	}

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "dev-secret-key-change-in-production"
	}

	token, err := jwt.ParseWithClaims(tokenStr, &jwt.MapClaims{}, func(t *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})

	if err != nil || !token.Valid {
		return c.Status(401).JSON(fiber.Map{"erro": "Token inválido ou expirado"})
	}

	claims := token.Claims.(*jwt.MapClaims)

	c.Locals("user_id", int((*claims)["user_id"].(float64)))
	c.Locals("username", (*claims)["username"].(string))

	return c.Next()
}
