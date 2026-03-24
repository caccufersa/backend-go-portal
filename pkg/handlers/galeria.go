package handlers

import (
	"cacc/pkg/repository"
	"cacc/pkg/services"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type GaleriaHandler struct {
	service    services.GaleriaService
	socialRepo repository.SocialRepository // para buscar displayName e avatar
}

func NewGaleria(s services.GaleriaService, socialRepo repository.SocialRepository) *GaleriaHandler {
	return &GaleriaHandler{service: s, socialRepo: socialRepo}
}

// GET /galeria/list?limit=30&offset=0
func (gh *GaleriaHandler) List(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 30)
	offset := c.QueryInt("offset", 0)

	items, err := gh.service.List(limit, offset)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao listar galeria"})
	}
	return c.JSON(items)
}

// POST /galeria/upload  (multipart/form-data: file + caption)
func (gh *GaleriaHandler) Upload(c *fiber.Ctx) error {
	userID, ok := c.Locals("user_id").(int)
	if !ok || userID == 0 {
		return c.Status(401).JSON(fiber.Map{"erro": "Não autenticado"})
	}
	username, _ := c.Locals("username").(string)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "Nenhum arquivo enviado (campo: file)"})
	}

	// Validar tipo
	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
	allowed := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true}
	if !allowed[ext] {
		return c.Status(400).JSON(fiber.Map{"erro": fmt.Sprintf("Tipo de arquivo não suportado: %s. Use jpg, png, gif ou webp.", ext)})
	}

	// Validar tamanho (máx 10 MB)
	const maxSize = 10 * 1024 * 1024
	if fileHeader.Size > maxSize {
		return c.Status(400).JSON(fiber.Map{"erro": "Arquivo muito grande (máx 10 MB)"})
	}

	f, err := fileHeader.Open()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao abrir arquivo"})
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": "Erro ao ler arquivo"})
	}

	caption := strings.TrimSpace(c.FormValue("caption"))
	if len(caption) > 300 {
		return c.Status(400).JSON(fiber.Map{"erro": "Legenda muito longa (máx 300 caracteres)"})
	}

	// Buscar display_name e avatar do usuário
	authorName := username
	avatarURL := ""
	un, displayName, _, avatar, _ := gh.socialRepo.ProfileInfo(userID)
	if un != "" {
		avatarURL = avatar
		if displayName != "" {
			authorName = displayName
		}
	}

	item, err := gh.service.Upload(data, fileHeader.Filename, userID, username, authorName, avatarURL, caption)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"erro": err.Error()})
	}

	return c.Status(201).JSON(item)
}

// DELETE /galeria/:id
func (gh *GaleriaHandler) Delete(c *fiber.Ctx) error {
	id, err := c.ParamsInt("id")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"erro": "ID inválido"})
	}

	userID, ok := c.Locals("user_id").(int)
	if !ok || userID == 0 {
		return c.Status(401).JSON(fiber.Map{"erro": "Não autenticado"})
	}

	if err := gh.service.Delete(id, userID); err != nil {
		if err.Error() == "imagem não encontrada" {
			return c.Status(404).JSON(fiber.Map{"erro": "Imagem não encontrada"})
		}
		return c.Status(403).JSON(fiber.Map{"erro": "Sem permissão para deletar esta imagem"})
	}

	return c.JSON(fiber.Map{"status": "deleted", "id": id})
}
