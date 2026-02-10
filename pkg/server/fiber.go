package server

import (
	"cacc/pkg/middleware"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func NewApp(name string) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:           name,
		ReduceMemoryUsage: true,
	})

	app.Use(compress.New(compress.Config{Level: compress.LevelBestSpeed}))
	app.Use(cors.New(middleware.CORSConfig()))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok", "service": name})
	})

	return app
}
