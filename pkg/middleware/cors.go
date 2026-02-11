package middleware

import (
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func CORSConfig() cors.Config {
	return cors.Config{
		AllowOrigins:     "http://localhost:3000,https://portal-cacc-frontend.vercel.app,https://localhost:5173",
		AllowMethods:     "POST,GET,DELETE,PUT,OPTIONS",
		AllowHeaders:     "Content-Type,Cache-Control,Pragma,Authorization,X-API-Key",
		AllowCredentials: true,
	}
}
