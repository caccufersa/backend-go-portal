package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"cacc/pkg/apperror"
	"cacc/pkg/models"
	"cacc/pkg/repository"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// ─── Constants ──────────────────────────────────────────────────────────────

const (
	accessTokenTTL  = 1 * time.Hour
	refreshTokenTTL = 30 * 24 * time.Hour
	resetTokenTTL   = 15 * time.Minute
	userCacheTTL    = 15 * time.Minute
	cacheCleanup    = 10 * time.Minute
	maxSessions     = 10
	bcryptCost      = bcrypt.DefaultCost
)

// ─── Interface ──────────────────────────────────────────────────────────────

type AuthService interface {
	Register(req models.RegisterRequest, userAgent, ip string) (models.AuthResponse, error)
	Login(req models.LoginRequest, userAgent, ip string) (models.AuthResponse, error)

	// Email-based password reset
	ForgotPassword(email string) error
	ResetPassword(req models.ResetPasswordRequest) error

	// Google OAuth
	GoogleOAuthURL(state string) string
	GoogleCallback(code, userAgent, ip string) (models.AuthResponse, error)

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

// ─── In-memory user cache ────────────────────────────────────────────────────

type cachedUser struct {
	User      models.User
	ExpiresAt time.Time
}

// ─── Implementation ─────────────────────────────────────────────────────────

type authService struct {
	repo        repository.AuthRepository
	emailSvc    EmailService
	jwtSecret   string
	oauthConfig *oauth2.Config
	frontendURL string

	mu     sync.RWMutex
	byID   map[int]*cachedUser
	byUUID map[string]*cachedUser
}

func NewAuthService(repo repository.AuthRepository, emailSvc EmailService, jwtSecret string) AuthService {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:3000"
	}

	oauthCfg := &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("GOOGLE_REDIRECT_URL"), // e.g. https://api.mydomain.com/auth/google/callback
		Scopes: []string{
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
			"openid",
		},
		Endpoint: google.Endpoint,
	}

	s := &authService{
		repo:        repo,
		emailSvc:    emailSvc,
		jwtSecret:   jwtSecret,
		oauthConfig: oauthCfg,
		frontendURL: frontendURL,
		byID:        make(map[int]*cachedUser),
		byUUID:      make(map[string]*cachedUser),
	}
	go s.cleanupUsers()
	return s
}

func (s *authService) GetJwtSecret() string { return s.jwtSecret }

// ─── Register ───────────────────────────────────────────────────────────────

func (s *authService) Register(req models.RegisterRequest, userAgent, ip string) (models.AuthResponse, error) {
	if err := validateUsername(req.Username); err != nil {
		return models.AuthResponse{}, err
	}
	if err := validatePassword(req.Password); err != nil {
		return models.AuthResponse{}, err
	}
	// Email is optional at registration but must be valid if provided
	if req.Email != "" {
		if err := validateEmail(req.Email); err != nil {
			return models.AuthResponse{}, err
		}
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		return models.AuthResponse{}, apperror.Internal("erro interno")
	}

	user, err := s.repo.CreateUser(req.Username, string(hashed), req.Email)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "duplicate key") || strings.Contains(msg, "users_username_key") {
			return models.AuthResponse{}, apperror.Conflict("username já existe")
		}
		if strings.Contains(msg, "users_email_key") {
			return models.AuthResponse{}, apperror.Conflict("e-mail já cadastrado")
		}
		return models.AuthResponse{}, apperror.Internal("erro ao criar conta")
	}

	s.setUser(user)
	return s.createSessionAndRespond(user, userAgent, ip)
}

// ─── Login ──────────────────────────────────────────────────────────────────

func (s *authService) Login(req models.LoginRequest, userAgent, ip string) (models.AuthResponse, error) {
	if req.Username == "" || req.Password == "" {
		return models.AuthResponse{}, apperror.Validation("username e senha obrigatórios")
	}

	user, hashedPw, err := s.repo.GetUserByUsername(req.Username)
	if err != nil {
		return models.AuthResponse{}, apperror.Unauthorized("username ou senha incorretos")
	}

	// Google-only accounts have no password
	if hashedPw == "" {
		return models.AuthResponse{}, apperror.Unauthorized("esta conta usa login com Google")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hashedPw), []byte(req.Password)); err != nil {
		return models.AuthResponse{}, apperror.Unauthorized("username ou senha incorretos")
	}

	s.setUser(user)
	return s.createSessionAndRespond(user, userAgent, ip)
}

// ─── Forgot Password (send e-mail) ──────────────────────────────────────────

func (s *authService) ForgotPassword(email string) error {
	if email == "" {
		return apperror.Validation("e-mail obrigatório")
	}
	if err := validateEmail(email); err != nil {
		return err
	}

	user, _, err := s.repo.GetUserByEmail(email)
	if err != nil {
		// Don't reveal whether the e-mail exists
		return nil
	}

	rawToken := generateSecureToken()
	tokenHash := hashToken(rawToken)
	expiresAt := time.Now().Add(resetTokenTTL)

	if err := s.repo.CreatePasswordResetToken(user.ID, tokenHash, expiresAt); err != nil {
		return apperror.Internal("erro ao gerar token de redefinição")
	}

	resetURL := fmt.Sprintf("%s/reset-password?token=%s", s.frontendURL, rawToken)

	// Send asynchronously so the HTTP response is fast
	go func() {
		if err := s.emailSvc.SendPasswordReset(user.Email, user.Username, resetURL); err != nil {
			// Log but don't fail – user can retry
			fmt.Printf("[AUTH] SendPasswordReset error for %s: %v\n", user.Email, err)
		} else {
			fmt.Printf("[AUTH] Password reset e-mail sent to %s\n", user.Email)
		}
	}()

	return nil
}

// ─── Reset Password (consume token from e-mail link) ────────────────────────

func (s *authService) ResetPassword(req models.ResetPasswordRequest) error {
	if req.Token == "" || req.NewPassword == "" {
		return apperror.Validation("token e nova senha são obrigatórios")
	}

	if err := validatePassword(req.NewPassword); err != nil {
		return err
	}

	tokenHash := hashToken(req.Token)

	userID, expiresAt, err := s.repo.GetPasswordResetToken(tokenHash)
	if err != nil {
		return apperror.Validation("token inválido ou expirado")
	}

	// Constant-time expiry check
	expired := subtle.ConstantTimeCompare(
		[]byte("1"),
		[]byte(map[bool]string{true: "0", false: "1"}[time.Now().Before(expiresAt)]),
	) != 1
	if expired {
		s.repo.DeletePasswordResetToken(tokenHash)
		return apperror.Validation("token expirado, solicite um novo")
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcryptCost)
	if err != nil {
		return apperror.Internal("erro interno")
	}

	if err := s.repo.UpdatePassword(userID, string(hashed)); err != nil {
		return apperror.Internal("erro ao redefinir a senha")
	}

	// Consume token + invalidate all sessions
	s.repo.DeletePasswordResetToken(tokenHash)
	s.deleteUserCache(userID)
	s.repo.DeleteAllSessionsByUserID(userID)

	return nil
}

// ─── Google OAuth ────────────────────────────────────────────────────────────

// GoogleOAuthURL returns the URL the client should redirect to.
func (s *authService) GoogleOAuthURL(state string) string {
	return s.oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// GoogleCallback exchanges the code, fetches the user profile, and creates a session.
func (s *authService) GoogleCallback(code, userAgent, ip string) (models.AuthResponse, error) {
	token, err := s.oauthConfig.Exchange(context.Background(), code)
	if err != nil {
		return models.AuthResponse{}, apperror.Unauthorized("código OAuth inválido")
	}

	accessToken := token.AccessToken
	if accessToken == "" {
		return models.AuthResponse{}, apperror.Internal("access_token ausente na resposta do Google")
	}

	profile, err := callGoogleUserinfo(accessToken)
	if err != nil {
		return models.AuthResponse{}, apperror.Unauthorized("falha ao obter perfil do Google")
	}

	user, err := s.repo.GetOrCreateGoogleUser(profile.Sub, profile.Email, profile.Name, profile.Picture)
	if err != nil {
		return models.AuthResponse{}, apperror.Internal("erro ao autenticar com Google")
	}

	s.setUser(user)
	return s.createSessionAndRespond(user, userAgent, ip)
}

// ─── Refresh ────────────────────────────────────────────────────────────────

func (s *authService) Refresh(refreshToken string) (models.AuthResponse, error) {
	if refreshToken == "" {
		return models.AuthResponse{}, apperror.Validation("refresh token não informado")
	}

	tokenHash := hashToken(refreshToken)
	session, user, err := s.repo.GetSessionByToken(tokenHash)
	if err != nil {
		return models.AuthResponse{}, apperror.Unauthorized("sessão inválida ou expirada")
	}

	if time.Now().After(session.ExpiresAt) {
		s.repo.DeleteSessionByID(session.ID)
		return models.AuthResponse{}, apperror.Unauthorized("sessão expirada, faça login novamente")
	}

	newRaw := generateRefreshToken()
	newHash := hashToken(newRaw)
	newExpiry := time.Now().Add(refreshTokenTTL)

	if err := s.repo.UpdateSession(session.ID, newHash, newExpiry); err != nil {
		return models.AuthResponse{}, apperror.Internal("erro interno")
	}

	accessToken := s.generateAccessToken(user)
	s.setUser(user)

	return models.AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: newRaw,
		User:         user,
		ExpiresIn:    int(accessTokenTTL.Seconds()),
	}, nil
}

// ─── Session ─────────────────────────────────────────────────────────────────

func (s *authService) Session(tokenStr, refreshToken string) (models.AuthResponse, error) {
	if tokenStr != "" {
		token, err := jwt.ParseWithClaims(tokenStr, &jwt.MapClaims{}, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method")
			}
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
			return models.AuthResponse{User: user}, nil
		}
	}

	if refreshToken == "" {
		return models.AuthResponse{}, apperror.Unauthorized("nenhuma sessão ativa")
	}

	tokenHash := hashToken(refreshToken)
	session, user, err := s.repo.GetSessionByToken(tokenHash)
	if err != nil || time.Now().After(session.ExpiresAt) {
		if err == nil {
			s.repo.DeleteSessionByID(session.ID)
		}
		return models.AuthResponse{}, apperror.Unauthorized("sessão expirada")
	}

	newRaw := generateRefreshToken()
	newHash := hashToken(newRaw)
	newExpiry := time.Now().Add(refreshTokenTTL)
	s.repo.UpdateSession(session.ID, newHash, newExpiry)

	accessToken := s.generateAccessToken(user)
	s.setUser(user)

	return models.AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: newRaw,
		User:         user,
		ExpiresIn:    int(accessTokenTTL.Seconds()),
	}, nil
}

// ─── Me ───────────────────────────────────────────────────────────────────────

func (s *authService) Me(userID int) (models.User, error) {
	if user, ok := s.getUser(userID); ok {
		return user, nil
	}
	user, err := s.repo.GetUserByID(userID)
	if err != nil {
		return models.User{}, apperror.NotFound("usuário não encontrado")
	}
	s.setUser(user)
	return user, nil
}

// ─── Logout ───────────────────────────────────────────────────────────────────

func (s *authService) Logout(refreshToken string, userID int) error {
	if refreshToken != "" {
		s.repo.DeleteSessionByToken(hashToken(refreshToken))
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
		return models.User{}, apperror.NotFound("usuário não encontrado")
	}
	s.setUser(user)
	return user, nil
}

func (s *authService) GetUserByIDObj(userID int) (models.User, bool) {
	return s.getUser(userID)
}

// ═══════════════════════════════════════════════════════════════════════════
//  Internal helpers
// ═══════════════════════════════════════════════════════════════════════════

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
	entry := &cachedUser{User: user, ExpiresAt: time.Now().Add(userCacheTTL)}
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
		time.Sleep(cacheCleanup)
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
	s.repo.EnforceSessionLimit(user.ID, maxSessions-1)

	rawRefresh := generateRefreshToken()
	tokenHash := hashToken(rawRefresh)
	expiresAt := time.Now().Add(refreshTokenTTL)

	if err := s.repo.CreateSession(user.ID, tokenHash, userAgent, ip, expiresAt); err != nil {
		return models.AuthResponse{}, apperror.Internal("erro ao criar sessão")
	}

	return models.AuthResponse{
		AccessToken:  s.generateAccessToken(user),
		RefreshToken: rawRefresh,
		User:         user,
		ExpiresIn:    int(accessTokenTTL.Seconds()),
	}, nil
}

func (s *authService) generateAccessToken(user models.User) string {
	now := time.Now()
	claims := jwt.MapClaims{
		"user_id":    user.ID,
		"uuid":       user.UUID,
		"username":   user.Username,
		"exp":        now.Add(accessTokenTTL).Unix(),
		"iat":        now.Unix(),
		"token_type": "access",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, _ := token.SignedString([]byte(s.jwtSecret))
	return tokenStr
}

// ─── Pure helpers ────────────────────────────────────────────────────────────

func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func generateRefreshToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// generateSecureToken produces a 32-byte URL-safe hex token for password reset links.
func generateSecureToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func validateUsername(u string) error {
	if len(u) < 3 {
		return apperror.Validation("username deve ter ao menos 3 caracteres")
	}
	if len(u) > 30 {
		return apperror.Validation("username muito longo (max 30)")
	}
	for _, r := range u {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return apperror.Validation("username só pode ter letras, números, _ e -")
		}
	}
	return nil
}

func validatePassword(p string) error {
	if len(p) < 8 {
		return apperror.Validation("senha deve ter ao menos 8 caracteres")
	}
	if len(p) > 128 {
		return apperror.Validation("senha muito longa")
	}
	return nil
}

func validateEmail(e string) error {
	at := strings.Index(e, "@")
	if at < 1 || at >= len(e)-1 {
		return apperror.Validation("e-mail inválido")
	}
	dot := strings.LastIndex(e[at:], ".")
	if dot < 1 {
		return apperror.Validation("e-mail inválido")
	}
	return nil
}
