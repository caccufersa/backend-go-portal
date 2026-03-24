package services

import (
	"cacc/pkg/cache"
	"cacc/pkg/models"
	"cacc/pkg/repository"
	"encoding/json"
	"fmt"
	"time"
)

type NoticiasService interface {
	Listar(categoria string, limit, offset int) ([]models.Noticia, error)
	BuscarPorID(id int) (models.Noticia, error)
	Destaques() ([]models.Noticia, error)
	Criar(req models.CriarNoticiaRequest, username string) (models.Noticia, error)
	Atualizar(id int, req models.AtualizarNoticiaRequest) (models.Noticia, error)
	Deletar(id int) (bool, error)
}

type noticiasService struct {
	repo  repository.NoticiasRepository
	redis *cache.Redis
}

func NewNoticiasService(repo repository.NoticiasRepository, redis *cache.Redis) NoticiasService {
	return &noticiasService{repo: repo, redis: redis}
}

func (s *noticiasService) Listar(categoria string, limit, offset int) ([]models.Noticia, error) {
	cacheKey := fmt.Sprintf("noticias:list:%s:%d:%d", categoria, limit, offset)
	var cached []models.Noticia
	if s.redis.Get(cacheKey, &cached) {
		return cached, nil
	}

	lista, err := s.repo.Listar(categoria, limit, offset)
	if err != nil {
		return nil, err
	}

	for i := range lista {
		parseEditorJS(&lista[i])
	}

	s.redis.Set(cacheKey, lista, 30*time.Second)
	return lista, nil
}

func (s *noticiasService) BuscarPorID(id int) (models.Noticia, error) {
	cacheKey := fmt.Sprintf("noticias:item:%d", id)
	var cached models.Noticia
	if s.redis.Get(cacheKey, &cached) {
		return cached, nil
	}

	noticia, err := s.repo.BuscarPorID(id)
	if err != nil {
		return noticia, err
	}

	parseEditorJS(&noticia)
	s.redis.Set(cacheKey, noticia, time.Minute)
	return noticia, nil
}

func (s *noticiasService) Destaques() ([]models.Noticia, error) {
	var cached []models.Noticia
	if s.redis.Get("noticias:destaques", &cached) {
		return cached, nil
	}

	lista, err := s.repo.Destaques()
	if err != nil {
		return nil, err
	}

	for i := range lista {
		parseEditorJS(&lista[i])
	}

	s.redis.Set("noticias:destaques", lista, 30*time.Second)
	return lista, nil
}

func (s *noticiasService) Criar(req models.CriarNoticiaRequest, username string) (models.Noticia, error) {
	conteudoStr, err := models.ParseConteudo(req.Conteudo)
	if err != nil {
		return models.Noticia{}, fmt.Errorf("formato de conteúdo inválido")
	}

	if req.Categoria == "" {
		req.Categoria = "Geral"
	}
	if req.Resumo == "" {
		req.Resumo = gerarResumo(conteudoStr)
	}

	if req.Author == "" {
		if username != "" {
			req.Author = username
		} else {
			req.Author = "Anônimo"
		}
	}

	nova := models.Noticia{
		Titulo:    req.Titulo,
		Conteudo:  conteudoStr,
		Resumo:    req.Resumo,
		Author:    req.Author,
		Categoria: req.Categoria,
		ImageURL:  req.ImageURL,
		Destaque:  req.Destaque,
		Tags:      req.Tags,
	}

	noticia, err := s.repo.Criar(nova)
	if err == nil {
		parseEditorJS(&noticia)
		s.redis.DelPattern("noticias:*")
	}

	return noticia, err
}

func (s *noticiasService) Atualizar(id int, req models.AtualizarNoticiaRequest) (models.Noticia, error) {
	var conteudoStr string
	if req.Conteudo != nil {
		parsed, err := models.ParseConteudo(req.Conteudo)
		if err != nil {
			return models.Noticia{}, fmt.Errorf("formato de conteúdo inválido")
		}
		conteudoStr = parsed
	}

	noticia, err := s.repo.Atualizar(id, req, conteudoStr)
	if err == nil {
		parseEditorJS(&noticia)
		s.redis.DelPattern("noticias:*")
	}

	return noticia, err
}

func (s *noticiasService) Deletar(id int) (bool, error) {
	deletado, err := s.repo.Deletar(id)
	if err == nil && deletado {
		s.redis.DelPattern("noticias:*")
	}
	return deletado, err
}

func parseEditorJS(noticia *models.Noticia) {
	var editorData models.EditorJSData
	if err := json.Unmarshal([]byte(noticia.Conteudo), &editorData); err == nil {
		noticia.ConteudoObj = &editorData
	}
}

func gerarResumo(conteudoStr string) string {
	var editorData models.EditorJSData
	if err := json.Unmarshal([]byte(conteudoStr), &editorData); err == nil {
		resumoText := ""
		for _, block := range editorData.Blocks {
			if blockType, ok := block["type"].(string); ok && (blockType == "paragraph" || blockType == "header") {
				if data, ok := block["data"].(map[string]interface{}); ok {
					if text, ok := data["text"].(string); ok {
						resumoText += text + " "
						if len(resumoText) > 200 {
							break
						}
					}
				}
			}
		}
		if len(resumoText) > 200 {
			return resumoText[:200] + "..."
		}
		if resumoText != "" {
			return resumoText
		}
		return "Nova notícia"
	}
	if len(conteudoStr) > 200 {
		return conteudoStr[:200] + "..."
	}
	return conteudoStr
}
