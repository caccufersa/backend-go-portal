package services

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"cacc/pkg/models"
	"cacc/pkg/repository"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type AuthService interface {
	Register(req models.RegisterRequest, userAgent, ip string) (models.AuthResponse, error)
	Login(req models.LoginRequest, userAgent, ip string) (models.AuthResponse, error)
	Refresh(refreshToken string) (models.AuthResponse, error)
	Session(tokenStr, refreshToken string) (models.AuthResponse, error)
	Me(userID int) (models.User, error)
	Logout(refreshToken string, userID int) error
	LogoutAll(userID int) error
	Sessions(userID int) ([]models.Session, error)
	GetUserByUUID(uuid string) (models.User, error)
	GetUserByIDObj(userID int) (models.User, bool)
	GetJwtSecret() string
}

type cachedUser struct {
	User      models.User
	ExpiresAt time.Time
}

type authService struct {
	repo      repository.AuthRepository
	jwtSecret string

	mu     sync.RWMutex
	byID   map[int]*cachedUser
	byUUID map[string]*cachedUser
}

func NewAuthService(repo repository.AuthRepository) AuthService {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "dev-secret-key-change-in-production"
	}

	s := &authService{
		repo:      repo,
		jwtSecret: secret,
		byID:      make(map[int]*cachedUser),
		byUUID:    make(map[string]*cachedUser),
	}
	go s.cleanupUsers()
	return s
}

func (s *authService) GetJwtSecret() string {
	return s.jwtSecret
}

func (s *authService) Register(req models.RegisterRequest, userAgent, ip string) (models.AuthResponse, error) {
	if err := validateUsername(req.Username); err != nil {
		return models.AuthResponse{}, err
	}
	if err := validatePassword(req.Password); err != nil {
		return models.AuthResponse{}, err
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return models.AuthResponse{}, fmt.Errorf("erro interno")
	}

	user, err := s.repo.CreateUser(req.Username, string(hashed))
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return models.AuthResponse{}, fmt.Errorf("username já existe")
		}
		return models.AuthResponse{}, fmt.Errorf("erro ao criar conta")
	}

	s.setUser(user)
	return s.createSessionAndRespond(user, userAgent, ip)
}

func (s *authService) Login(req models.LoginRequest, userAgent, ip string) (models.AuthResponse, error) {
	if req.Username == "" || req.Password == "" {
		return models.AuthResponse{}, fmt.Errorf("username e senha obrigatórios")
	}

	user, hashedPw, err := s.repo.GetUserByUsername(req.Username)
	if err != nil {
		return models.AuthResponse{}, fmt.Errorf("username ou senha incorretos")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hashedPw), []byte(req.Password)); err != nil {
		return models.AuthResponse{}, fmt.Errorf("username ou senha incorretos")
	}

	s.setUser(user)
	return s.createSessionAndRespond(user, userAgent, ip)
}

func (s *authService) Refresh(refreshToken string) (models.AuthResponse, error) {
	if refreshToken == "" {
		return models.AuthResponse{}, fmt.Errorf("refresh token não informado")
	}

	session, user, err := s.repo.GetSessionByToken(refreshToken)
	if err != nil {
		return models.AuthResponse{}, fmt.Errorf("sessão inválida ou expirada")
	}

	if time.Now().After(session.ExpiresAt) {
		s.repo.DeleteSessionByID(session.ID)
		return models.AuthResponse{}, fmt.Errorf("sessão expirada, faça login novamente")
	}

	newRefresh := generateRefreshToken()
	newExpiry := time.Now().Add(30 * 24 * time.Hour)

	if err := s.repo.UpdateSession(session.ID, newRefresh, newExpiry); err != nil {
		return models.AuthResponse{}, fmt.Errorf("erro interno")
	}

	accessToken := s.generateAccessToken(user)
	s.setUser(user)

	return models.AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: newRefresh,
		User:         user,
		ExpiresIn:    3600,
	}, nil
}

func (s *authService) Session(tokenStr, refreshToken string) (models.AuthResponse, error) {
	if tokenStr != "" {
		token, err := jwt.ParseWithClaims(tokenStr, &jwt.MapClaims{}, func(t *jwt.Token) (interface{}, error) {
			return []byte(s.jwtSecret), nil
		})
		if err == nil && token.Valid {
			claims := token.Claims.(*jwt.MapClaims)
			userID := int((*claims)["user_id"].(float64))
			userUUID, _ := (*claims)["uuid"].(string)
			username := (*claims)["username"].(string)

			user, ok := s.getUser(userID)
			if !ok {
				user = models.User{ID: userID, UUID: userUUID, Username: username}
			}
			return models.AuthResponse{
				User: user,
			}, nil
		}
	}

	if refreshToken == "" {
		return models.AuthResponse{}, fmt.Errorf("nenhuma sessão ativa")
	}

	session, user, err := s.repo.GetSessionByToken(refreshToken)
	if err != nil || time.Now().After(session.ExpiresAt) {
		if err == nil {
			s.repo.DeleteSessionByID(session.ID)
		}
		return models.AuthResponse{}, fmt.Errorf("sessão expirada")
	}

	newRefresh := generateRefreshToken()
	newExpiry := time.Now().Add(30 * 24 * time.Hour)
	s.repo.UpdateSession(session.ID, newRefresh, newExpiry)

	accessToken := s.generateAccessToken(user)
	s.setUser(user)

	return models.AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: newRefresh,
		User:         user,
		ExpiresIn:    3600,
	}, nil
}

func (s *authService) Me(userID int) (models.User, error) {
	if user, ok := s.getUser(userID); ok {
		return user, nil
	}

	user, err := s.repo.GetUserByID(userID)
	if err != nil {
		return models.User{}, fmt.Errorf("usuário não encontrado")
	}

	s.setUser(user)
	return user, nil
}

func (s *authService) Logout(refreshToken string, userID int) error {
	if refreshToken != "" {
		s.repo.DeleteSessionByToken(refreshToken)
	}

	if userID > 0 {
		s.deleteUserCache(userID)
	}

	return nil
}

func (s *authService) LogoutAll(userID int) error {
	err := s.repo.DeleteAllSessionsByUserID(userID)
	s.deleteUserCache(userID)
	return err
}

func (s *authService) Sessions(userID int) ([]models.Session, error) {
	return s.repo.GetActiveSessionsByUserID(userID)
}

func (s *authService) GetUserByUUID(uuid string) (models.User, error) {
	s.mu.RLock()
	if item, ok := s.byUUID[uuid]; ok && time.Now().Before(item.ExpiresAt) {
		s.mu.RUnlock()
		return item.User, nil
	}
	s.mu.RUnlock()

	user, err := s.repo.GetUserByUUID(uuid)
	if err != nil {
		return models.User{}, fmt.Errorf("usuário não encontrado")
	}
	return user, nil
}

func (s *authService) GetUserByIDObj(userID int) (models.User, bool) {
	return s.getUser(userID)
}

// Internal Helpers
func (s *authService) getUser(id int) (models.User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if item, ok := s.byID[id]; ok && time.Now().Before(item.ExpiresAt) {
		return item.User, true
	}
	return models.User{}, false
}

func (s *authService) setUser(user models.User) {
	s.mu.Lock()
	entry := &cachedUser{User: user, ExpiresAt: time.Now().Add(15 * time.Minute)}
	s.byID[user.ID] = entry
	if user.UUID != "" {
		s.byUUID[user.UUID] = entry
	}
	s.mu.Unlock()
}

func (s *authService) deleteUserCache(id int) {
	s.mu.Lock()
	if item, ok := s.byID[id]; ok {
		delete(s.byUUID, item.User.UUID)
	}
	delete(s.byID, id)
	s.mu.Unlock()
}

func (s *authService) cleanupUsers() {
	for {
		time.Sleep(10 * time.Minute)
		s.mu.Lock()
		now := time.Now()
		for k, v := range s.byID {
			if now.After(v.ExpiresAt) {
				delete(s.byUUID, v.User.UUID)
				delete(s.byID, k)
			}
		}
		s.mu.Unlock()
	}
}

func (s *authService) createSessionAndRespond(user models.User, userAgent, ip string) (models.AuthResponse, error) {
	accessToken := s.generateAccessToken(user)
	refreshToken := generateRefreshToken()
	expiresAt := time.Now().Add(30 * 24 * time.Hour)

	err := s.repo.CreateSession(user.ID, refreshToken, userAgent, ip, expiresAt)
	if err != nil {
		return models.AuthResponse{}, fmt.Errorf("erro ao criar sessão")
	}

	return models.AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User:         user,
		ExpiresIn:    3600,
	}, nil
}

func (s *authService) generateAccessToken(user models.User) string {
	claims := jwt.MapClaims{
		"user_id":    user.ID,
		"uuid":       user.UUID,
		"username":   user.Username,
		"exp":        time.Now().Add(1 * time.Hour).Unix(),
		"iat":        time.Now().Unix(),
		"token_type": "access",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, _ := token.SignedString([]byte(s.jwtSecret))
	return tokenStr
}

func generateRefreshToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func validateUsername(u string) error {
	if len(u) < 3 {
		return fmt.Errorf("username deve ter ao menos 3 caracteres")
	}
	if len(u) > 30 {
		return fmt.Errorf("username muito longo (max 30)")
	}
	for _, r := range u {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return fmt.Errorf("username só pode ter letras, números, _ e -")
		}
	}
	return nil
}

func validatePassword(p string) error {
	if len(p) < 8 {
		return fmt.Errorf("senha deve ter ao menos 8 caracteres")
	}
	if len(p) > 128 {
		return fmt.Errorf("senha muito longa")
	}
	return nil
}
