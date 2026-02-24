package middleware

import (
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

func parseJWT(tokenStr string) (userID int, userUUID, username string, ok bool) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "dev-secret-key-change-in-production"
	}
	token, err := jwt.ParseWithClaims(tokenStr, &jwt.MapClaims{}, func(t *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return 0, "", "", false
	}
	claims := token.Claims.(*jwt.MapClaims)
	userID = int((*claims)["user_id"].(float64))
	userUUID, _ = (*claims)["uuid"].(string)
	username, _ = (*claims)["username"].(string)
	return userID, userUUID, username, true
}

// AuthMiddleware exige token JWT válido; retorna 401 se ausente ou inválido.
func AuthMiddleware(c *fiber.Ctx) error {
	auth := c.Get("Authorization")
	if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
		return c.Status(401).JSON(fiber.Map{"erro": "Token não informado"})
	}
	userID, userUUID, username, ok := parseJWT(auth[7:])
	if !ok {
		return c.Status(401).JSON(fiber.Map{"erro": "Token inválido"})
	}
	c.Locals("user_id", userID)
	c.Locals("user_uuid", userUUID)
	c.Locals("username", username)
	return c.Next()
}

// OptionalAuthMiddleware popula os locals do usuário se houver token válido,
// mas NÃO bloqueia a requisição se não houver token – útil para rotas públicas
// que exibem conteúdo extra quando o usuário está logado.
func OptionalAuthMiddleware(c *fiber.Ctx) error {
	auth := c.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		if userID, userUUID, username, ok := parseJWT(auth[7:]); ok {
			c.Locals("user_id", userID)
			c.Locals("user_uuid", userUUID)
			c.Locals("username", username)
		}
	}
	return c.Next()
}

func AdminMiddleware(c *fiber.Ctx) error {
	adminKey := c.Get("X-Admin-Key")
	expectedKey := os.Getenv("ADMIN_SECRET_KEY")

	if expectedKey == "" {
		expectedKey = "dev-admin-secret"
	}

	if adminKey != expectedKey {
		return c.Status(403).JSON(fiber.Map{"erro": "Acesso negado: Chave administrativa secreta inválida"})
	}

	return c.Next()
}
