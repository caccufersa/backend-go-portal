package services

import (
	"bytes"
	"cacc/pkg/models"
	"cacc/pkg/repository"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type GaleriaService interface {
	List(limit, offset int) ([]models.GaleriaItem, error)
	Upload(fileData []byte, fileName string, userID int, author, authorName, avatarURL, caption string) (models.GaleriaItem, error)
	Delete(id, userID int) error
}

type galeriaService struct {
	repo repository.GaleriaRepository
}

func NewGaleriaService(repo repository.GaleriaRepository) GaleriaService {
	return &galeriaService{repo: repo}
}

func (s *galeriaService) List(limit, offset int) ([]models.GaleriaItem, error) {
	return s.repo.List(limit, offset)
}

func (s *galeriaService) Upload(fileData []byte, fileName string, userID int, author, authorName, avatarURL, caption string) (models.GaleriaItem, error) {
	cloudName := os.Getenv("CLOUDINARY_CLOUD_NAME")
	apiKey := os.Getenv("CLOUDINARY_API_KEY")
	apiSecret := os.Getenv("CLOUDINARY_API_SECRET")

	if cloudName == "" || apiKey == "" || apiSecret == "" {
		return models.GaleriaItem{}, fmt.Errorf("cloudinary não configurado: defina CLOUDINARY_CLOUD_NAME, CLOUDINARY_API_KEY e CLOUDINARY_API_SECRET")
	}

	// Parâmetros para a assinatura Cloudinary
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	folder := "galeria"

	// Gerar assinatura HMAC-SHA1
	params := map[string]string{
		"folder":    folder,
		"timestamp": timestamp,
	}
	sig := cloudinarySign(params, apiSecret)

	// Montar multipart form
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	if fw, err := mw.CreateFormFile("file", fileName); err == nil {
		fw.Write(fileData)
	}
	mw.WriteField("api_key", apiKey)
	mw.WriteField("timestamp", timestamp)
	mw.WriteField("folder", folder)
	mw.WriteField("signature", sig)
	mw.Close()

	uploadURL := fmt.Sprintf("https://api.cloudinary.com/v1_1/%s/image/upload", cloudName)
	req, err := http.NewRequest("POST", uploadURL, &buf)
	if err != nil {
		return models.GaleriaItem{}, fmt.Errorf("erro ao criar request cloudinary: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return models.GaleriaItem{}, fmt.Errorf("erro ao fazer upload: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return models.GaleriaItem{}, fmt.Errorf("cloudinary retornou status %d: %s", resp.StatusCode, string(body))
	}

	var cloudResp struct {
		SecureURL string `json:"secure_url"`
		PublicID  string `json:"public_id"`
	}
	if err := json.Unmarshal(body, &cloudResp); err != nil {
		return models.GaleriaItem{}, fmt.Errorf("erro ao parsear resposta cloudinary: %w", err)
	}

	return s.repo.Create(userID, author, authorName, avatarURL, cloudResp.SecureURL, cloudResp.PublicID, caption)
}

func (s *galeriaService) Delete(id, userID int) error {
	item, err := s.repo.GetByID(id)
	if err != nil {
		return fmt.Errorf("imagem não encontrada")
	}

	// Deleção no Cloudinary (best-effort)
	if item.PublicID != "" {
		cloudName := os.Getenv("CLOUDINARY_CLOUD_NAME")
		apiKey := os.Getenv("CLOUDINARY_API_KEY")
		apiSecret := os.Getenv("CLOUDINARY_API_SECRET")

		if cloudName != "" && apiKey != "" && apiSecret != "" {
			timestamp := strconv.FormatInt(time.Now().Unix(), 10)
			params := map[string]string{
				"public_id": item.PublicID,
				"timestamp": timestamp,
			}
			sig := cloudinarySign(params, apiSecret)

			data := url.Values{}
			data.Set("public_id", item.PublicID)
			data.Set("api_key", apiKey)
			data.Set("timestamp", timestamp)
			data.Set("signature", sig)

			destroyURL := fmt.Sprintf("https://api.cloudinary.com/v1_1/%s/image/destroy", cloudName)
			http.Post(destroyURL, "application/x-www-form-urlencoded", strings.NewReader(data.Encode())) //nolint
		}
	}

	return s.repo.Delete(id, userID)
}

// cloudinarySign gera assinatura SHA1 para requisições autenticadas da Cloudinary API.
// Protocolo: SHA1(param1=v1&param2=v2...{api_secret})  — NÃO é HMAC.
func cloudinarySign(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(params))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	toSign := strings.Join(parts, "&") + secret

	h := sha1.New()
	h.Write([]byte(toSign))
	return hex.EncodeToString(h.Sum(nil))
}
