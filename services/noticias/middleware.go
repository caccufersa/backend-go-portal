package main

import (
	"encoding/base64"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func EditorAuthMiddleware(c *fiber.Ctx) error {
	user := os.Getenv("NOTICIAS_EDITOR_USERNAME")
	pass := os.Getenv("NOTICIAS_EDITOR_PASSWORD")
	apiKey := os.Getenv("NOTICIAS_API_KEY")

	auth := c.Get("Authorization")
	if strings.HasPrefix(auth, "Basic ") {
		payload := auth[6:]
		decoded, err := decodeBase64(payload)
		if err == nil {
			parts := strings.SplitN(decoded, ":", 2)
			if len(parts) == 2 && parts[0] == user && parts[1] == pass {
				return c.Next()
			}
		}
	}

	if apiKey != "" && c.Get("X-API-Key") == apiKey {
		return c.Next()
	}

	return c.Status(401).JSON(fiber.Map{"erro": "Editor não autorizado"})
}

func decodeBase64(s string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
