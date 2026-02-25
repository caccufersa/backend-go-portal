package middleware

import (
	"crypto/subtle"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

var jwtSecretBytes []byte
var adminKeyBytes []byte

// InitSecrets must be called once at startup to inject secrets.
// This avoids reading os.Getenv on every request.
func InitSecrets(jwtSecret, adminKey string) {
	jwtSecretBytes = []byte(jwtSecret)
	adminKeyBytes = []byte(adminKey)
}

func parseJWT(tokenStr string) (userID int, userUUID, username string, ok bool) {
	token, err := jwt.ParseWithClaims(tokenStr, &jwt.MapClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, hmac := t.Method.(*jwt.SigningMethodHMAC); !hmac {
			return nil, fiber.ErrUnauthorized
		}
		return jwtSecretBytes, nil
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

// AuthMiddleware requires a valid JWT; returns 401 otherwise.
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

// OptionalAuthMiddleware populates user locals if a valid token is present,
// but does NOT block the request – useful for public routes with extra info
// when the user is logged in.
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

// AdminMiddleware validates the X-Admin-Key header using constant-time comparison.
func AdminMiddleware(c *fiber.Ctx) error {
	provided := []byte(c.Get("X-Admin-Key"))
	if subtle.ConstantTimeCompare(provided, adminKeyBytes) != 1 {
		return c.Status(403).JSON(fiber.Map{"erro": "Acesso negado: Chave administrativa secreta inválida"})
	}
	return c.Next()
}
