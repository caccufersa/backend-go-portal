package middleware

import (
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func CORSConfig() cors.Config {
	return cors.Config{
		AllowOrigins: "http://localhost:3000,https://cacc-frontend.vercel.app",
		AllowMethods: "POST,GET,DELETE,PUT,OPTIONS",
		AllowHeaders: "Content-Type,Cache-Control,Pragma,Authorization",
	}
}
