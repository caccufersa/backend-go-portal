package middleware

import (
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func CORSConfig() cors.Config {
	return cors.Config{
		AllowOrigins:     "http://localhost:3000,http://localhost:5173,https://portal-cacc-frontend.vercel.app",
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders:     "Content-Type,Cache-Control,Pragma,Authorization,X-API-Key",
		AllowCredentials: true,
		ExposeHeaders:    "Content-Length,Content-Type",
		MaxAge:           3600,
	}
}
