package repository

import (
	"cacc/pkg/models"
	"database/sql"
	"strings"
	"time"
)

type AuthRepository interface {
	// Users
	CreateUser(username, hashedPassword, email string) (models.User, error)
	GetUserByUsername(username string) (models.User, string, error)
	GetUserByEmail(email string) (models.User, string, error)
	GetUserByID(id int) (models.User, error)
	GetUserByUUID(uuid string) (models.User, error)
	UpdatePassword(userID int, newHashedPassword string) error

	// Google OAuth
	GetOrCreateGoogleUser(googleID, email, username string) (models.User, error)

	// Password reset tokens
	CreatePasswordResetToken(userID int, tokenHash string, expiresAt time.Time) error
	GetPasswordResetToken(tokenHash string) (userID int, expiresAt time.Time, err error)
	DeletePasswordResetToken(tokenHash string) error
	DeleteExpiredPasswordResetTokens() error

	// Sessions
	CreateSession(userID int, tokenHash, userAgent, ip string, expiresAt time.Time) error
	GetSessionByToken(tokenHash string) (models.Session, models.User, error)
	UpdateSession(sessionID int, newTokenHash string, expiresAt time.Time) error
	DeleteSessionByID(sessionID int) error
	DeleteSessionByToken(tokenHash string) error
	DeleteAllSessionsByUserID(userID int) error
	GetActiveSessionsByUserID(userID int) ([]models.Session, error)
	EnforceSessionLimit(userID int, maxSessions int) error
}

type authRepository struct {
	db *sql.DB
}

func NewAuthRepository(db *sql.DB) AuthRepository {
	return &authRepository{db: db}
}

// ─── Users ───────────────────────────────────────────────────────────────────

func (r *authRepository) CreateUser(username, hashedPassword, email string) (models.User, error) {
	var user models.User
	var emailOut sql.NullString
	err := r.db.QueryRow(
		`INSERT INTO users (username, password, email)
		 VALUES ($1, $2, NULLIF($3,''))
		 RETURNING id, uuid, username, COALESCE(email,''), created_at`,
		strings.ToLower(username), hashedPassword, email,
	).Scan(&user.ID, &user.UUID, &user.Username, &emailOut, &user.CreatedAt)
	if emailOut.Valid {
		user.Email = emailOut.String
	}
	return user, err
}

func (r *authRepository) GetUserByUsername(username string) (models.User, string, error) {
	var user models.User
	var hashedPw string
	var email sql.NullString
	err := r.db.QueryRow(
		`SELECT id, uuid, username, password, COALESCE(email,''), created_at
		 FROM users WHERE username = $1`,
		strings.ToLower(username),
	).Scan(&user.ID, &user.UUID, &user.Username, &hashedPw, &email, &user.CreatedAt)
	if email.Valid {
		user.Email = email.String
	}
	return user, hashedPw, err
}

func (r *authRepository) GetUserByEmail(email string) (models.User, string, error) {
	var user models.User
	var hashedPw string
	var pw sql.NullString
	err := r.db.QueryRow(
		`SELECT id, uuid, username, COALESCE(password,''), COALESCE(email,''), created_at
		 FROM users WHERE lower(email) = lower($1)`,
		email,
	).Scan(&user.ID, &user.UUID, &user.Username, &pw, &user.Email, &user.CreatedAt)
	if pw.Valid {
		hashedPw = pw.String
	}
	return user, hashedPw, err
}

func (r *authRepository) UpdatePassword(userID int, newHashedPassword string) error {
	_, err := r.db.Exec(`UPDATE users SET password = $1 WHERE id = $2`, newHashedPassword, userID)
	return err
}

func (r *authRepository) GetUserByID(id int) (models.User, error) {
	var user models.User
	var email sql.NullString
	err := r.db.QueryRow(
		`SELECT id, uuid, username, COALESCE(email,''), created_at FROM users WHERE id = $1`, id,
	).Scan(&user.ID, &user.UUID, &user.Username, &email, &user.CreatedAt)
	if email.Valid {
		user.Email = email.String
	}
	return user, err
}

func (r *authRepository) GetUserByUUID(uuid string) (models.User, error) {
	var user models.User
	var email sql.NullString
	err := r.db.QueryRow(
		`SELECT id, uuid, username, COALESCE(email,''), created_at FROM users WHERE uuid = $1`, uuid,
	).Scan(&user.ID, &user.UUID, &user.Username, &email, &user.CreatedAt)
	if email.Valid {
		user.Email = email.String
	}
	return user, err
}

// ─── Google OAuth ─────────────────────────────────────────────────────────────

// GetOrCreateGoogleUser finds a user by google_id or email, creating one if needed.
func (r *authRepository) GetOrCreateGoogleUser(googleID, email, username string) (models.User, error) {
	var user models.User
	var emailOut sql.NullString

	// 1. Existing Google-linked account
	err := r.db.QueryRow(
		`SELECT id, uuid, username, COALESCE(email,''), created_at
		 FROM users WHERE google_id = $1`,
		googleID,
	).Scan(&user.ID, &user.UUID, &user.Username, &emailOut, &user.CreatedAt)
	if err == nil {
		if emailOut.Valid {
			user.Email = emailOut.String
		}
		return user, nil
	}

	// 2. Account with same email – link the google_id
	err = r.db.QueryRow(
		`UPDATE users SET google_id = $1
		 WHERE lower(email) = lower($2)
		 RETURNING id, uuid, username, COALESCE(email,''), created_at`,
		googleID, email,
	).Scan(&user.ID, &user.UUID, &user.Username, &emailOut, &user.CreatedAt)
	if err == nil {
		if emailOut.Valid {
			user.Email = emailOut.String
		}
		return user, nil
	}

	// 3. Brand new user (no password, Google-only)
	safeUsername := sanitizeUsername(username)
	err = r.db.QueryRow(
		`INSERT INTO users (username, password, email, google_id)
		 VALUES ($1, '', $2, $3)
		 ON CONFLICT (username) DO UPDATE
		   SET username = users.username || '_' || substr(md5(random()::text),1,4)
		 RETURNING id, uuid, username, COALESCE(email,''), created_at`,
		safeUsername, email, googleID,
	).Scan(&user.ID, &user.UUID, &user.Username, &emailOut, &user.CreatedAt)
	if emailOut.Valid {
		user.Email = emailOut.String
	}
	return user, err
}

// sanitizeUsername keeps only safe characters and trims length.
func sanitizeUsername(s string) string {
	var out []rune
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			out = append(out, r)
		}
	}
	if len(out) > 30 {
		out = out[:30]
	}
	if len(out) < 3 {
		out = append(out, []rune("user")...)
	}
	return string(out)
}

// ─── Password Reset Tokens ───────────────────────────────────────────────────

func (r *authRepository) CreatePasswordResetToken(userID int, tokenHash string, expiresAt time.Time) error {
	// One token per user at a time – replace any existing one
	_, err := r.db.Exec(
		`INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id) DO UPDATE
		   SET token_hash = EXCLUDED.token_hash, expires_at = EXCLUDED.expires_at, created_at = NOW()`,
		userID, tokenHash, expiresAt,
	)
	return err
}

func (r *authRepository) GetPasswordResetToken(tokenHash string) (int, time.Time, error) {
	var userID int
	var expiresAt time.Time
	err := r.db.QueryRow(
		`SELECT user_id, expires_at FROM password_reset_tokens WHERE token_hash = $1`,
		tokenHash,
	).Scan(&userID, &expiresAt)
	return userID, expiresAt, err
}

func (r *authRepository) DeletePasswordResetToken(tokenHash string) error {
	_, err := r.db.Exec(`DELETE FROM password_reset_tokens WHERE token_hash = $1`, tokenHash)
	return err
}

func (r *authRepository) DeleteExpiredPasswordResetTokens() error {
	_, err := r.db.Exec(`DELETE FROM password_reset_tokens WHERE expires_at < NOW()`)
	return err
}

// ─── Sessions ────────────────────────────────────────────────────────────────

func (r *authRepository) CreateSession(userID int, tokenHash, userAgent, ip string, expiresAt time.Time) error {
	_, err := r.db.Exec(
		`INSERT INTO sessions (user_id, refresh_token, user_agent, ip, expires_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		userID, tokenHash, userAgent, ip, expiresAt,
	)
	return err
}

func (r *authRepository) GetSessionByToken(tokenHash string) (models.Session, models.User, error) {
	var session models.Session
	var user models.User
	var email sql.NullString
	err := r.db.QueryRow(
		`SELECT s.id, s.user_id, s.expires_at, u.uuid, u.username, COALESCE(u.email,''), u.created_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.refresh_token = $1`, tokenHash,
	).Scan(&session.ID, &session.UserID, &session.ExpiresAt, &user.UUID, &user.Username, &email, &user.CreatedAt)
	user.ID = session.UserID
	if email.Valid {
		user.Email = email.String
	}
	return session, user, err
}

func (r *authRepository) UpdateSession(sessionID int, newTokenHash string, expiresAt time.Time) error {
	_, err := r.db.Exec(
		`UPDATE sessions SET refresh_token = $1, expires_at = $2 WHERE id = $3`,
		newTokenHash, expiresAt, sessionID,
	)
	return err
}

func (r *authRepository) DeleteSessionByID(sessionID int) error {
	_, err := r.db.Exec(`DELETE FROM sessions WHERE id = $1`, sessionID)
	return err
}

func (r *authRepository) DeleteSessionByToken(tokenHash string) error {
	_, err := r.db.Exec(`DELETE FROM sessions WHERE refresh_token = $1`, tokenHash)
	return err
}

func (r *authRepository) DeleteAllSessionsByUserID(userID int) error {
	_, err := r.db.Exec(`DELETE FROM sessions WHERE user_id = $1`, userID)
	return err
}

func (r *authRepository) GetActiveSessionsByUserID(userID int) ([]models.Session, error) {
	rows, err := r.db.Query(
		`SELECT id, user_agent, ip, expires_at, created_at FROM sessions
		 WHERE user_id = $1 AND expires_at > NOW() ORDER BY created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []models.Session
	for rows.Next() {
		var s models.Session
		if err := rows.Scan(&s.ID, &s.UserAgent, &s.IP, &s.ExpiresAt, &s.CreatedAt); err == nil {
			sessions = append(sessions, s)
		}
	}
	return sessions, nil
}

func (r *authRepository) EnforceSessionLimit(userID int, maxSessions int) error {
	_, err := r.db.Exec(
		`DELETE FROM sessions
		 WHERE id IN (
		   SELECT id FROM sessions
		   WHERE user_id = $1
		   ORDER BY created_at DESC
		   OFFSET $2
		 )`, userID, maxSessions,
	)
	return err
}
