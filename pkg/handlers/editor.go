package handlers

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

type LinkMetadata struct {
	Success int  `json:"success"`
	Meta    Meta `json:"meta"`
}

type Meta struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Image       Image  `json:"image"`
}

type Image struct {
	URL string `json:"url"`
}

func FetchLinkMeta(c *fiber.Ctx) error {
	var req struct {
		URL string `json:"url"`
	}

	if err := c.BodyParser(&req); err != nil || req.URL == "" {
		return c.Status(400).JSON(fiber.Map{"success": 0, "erro": "URL inválida"})
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(req.URL)
	if err != nil || resp.StatusCode != http.StatusOK {
		return c.Status(500).JSON(fiber.Map{"success": 0, "erro": "URL não acessível"})
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"success": 0, "erro": "Erro ao ler conteúdo"})
	}

	html := string(body)
	title := extractMetaTag(html, "og:title")
	if title == "" {
		title = extractTitleTag(html)
	}
	description := extractMetaTag(html, "og:description")
	if description == "" {
		description = extractMetaTag(html, "description")
	}
	imageURL := extractMetaTag(html, "og:image")

	return c.JSON(LinkMetadata{
		Success: 1,
		Meta: Meta{
			Title: title, Description: description, Image: Image{URL: imageURL},
		},
	})
}

func UploadImage(c *fiber.Ctx) error {
	file, err := c.FormFile("image")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"success": 0, "erro": "Nenhuma imagem fornecida"})
	}

	contentType := file.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		return c.Status(400).JSON(fiber.Map{"success": 0, "erro": "Arquivo não é uma imagem"})
	}

	imageURL := "https://example.com/uploads/" + file.Filename
	return c.JSON(fiber.Map{"success": 1, "file": fiber.Map{"url": imageURL}})
}

func extractMetaTag(html, property string) string {
	patterns := []string{
		`<meta\s+property=["']` + property + `["']\s+content=["']([^"']+)["']`,
		`<meta\s+content=["']([^"']+)["']\s+property=["']` + property + `["']`,
		`<meta\s+name=["']` + property + `["']\s+content=["']([^"']+)["']`,
		`<meta\s+content=["']([^"']+)["']\s+name=["']` + property + `["']`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(html)
		if len(matches) > 1 {
			return strings.TrimSpace(matches[1])
		}
	}
	return ""
}

func extractTitleTag(html string) string {
	re := regexp.MustCompile(`<title>([^<]+)</title>`)
	matches := re.FindStringSubmatch(html)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}
