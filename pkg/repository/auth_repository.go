package repository

import (
	"cacc/pkg/models"
	"database/sql"
	"strings"
	"time"
)

type AuthRepository interface {
	CreateUser(username, hashedPassword string) (models.User, error)
	GetUserByUsername(username string) (models.User, string, error)
	GetUserByID(id int) (models.User, error)
	GetUserByUUID(uuid string) (models.User, error)
	CreateSession(userID int, refreshToken, userAgent, ip string, expiresAt time.Time) error
	GetSessionByToken(token string) (models.Session, models.User, error)
	UpdateSession(sessionID int, newRefresh string, expiresAt time.Time) error
	DeleteSessionByID(sessionID int) error
	DeleteSessionByToken(token string) error
	DeleteAllSessionsByUserID(userID int) error
	GetActiveSessionsByUserID(userID int) ([]models.Session, error)
}

type authRepository struct {
	db *sql.DB
}

func NewAuthRepository(db *sql.DB) AuthRepository {
	return &authRepository{db: db}
}

func (r *authRepository) CreateUser(username, hashedPassword string) (models.User, error) {
	var user models.User
	err := r.db.QueryRow(
		`INSERT INTO users (username, password) VALUES ($1, $2)
		 RETURNING id, uuid, username, created_at`,
		strings.ToLower(username), hashedPassword,
	).Scan(&user.ID, &user.UUID, &user.Username, &user.CreatedAt)
	return user, err
}

func (r *authRepository) GetUserByUsername(username string) (models.User, string, error) {
	var user models.User
	var hashedPw string
	err := r.db.QueryRow(
		`SELECT id, uuid, username, password, created_at FROM users WHERE username = $1`,
		strings.ToLower(username),
	).Scan(&user.ID, &user.UUID, &user.Username, &hashedPw, &user.CreatedAt)
	return user, hashedPw, err
}

func (r *authRepository) GetUserByID(id int) (models.User, error) {
	var user models.User
	err := r.db.QueryRow(
		`SELECT id, uuid, username, created_at FROM users WHERE id = $1`, id,
	).Scan(&user.ID, &user.UUID, &user.Username, &user.CreatedAt)
	return user, err
}

func (r *authRepository) GetUserByUUID(uuid string) (models.User, error) {
	var user models.User
	err := r.db.QueryRow(
		`SELECT id, username, uuid, created_at FROM users WHERE uuid = $1`, uuid,
	).Scan(&user.ID, &user.Username, &user.UUID, &user.CreatedAt)
	return user, err
}

func (r *authRepository) CreateSession(userID int, refreshToken, userAgent, ip string, expiresAt time.Time) error {
	_, err := r.db.Exec(
		`INSERT INTO sessions (user_id, refresh_token, user_agent, ip, expires_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		userID, refreshToken, userAgent, ip, expiresAt,
	)
	return err
}

func (r *authRepository) GetSessionByToken(token string) (models.Session, models.User, error) {
	var session models.Session
	var user models.User
	err := r.db.QueryRow(
		`SELECT s.id, s.user_id, s.expires_at, u.uuid, u.username, u.created_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.refresh_token = $1`, token,
	).Scan(&session.ID, &session.UserID, &session.ExpiresAt, &user.UUID, &user.Username, &user.CreatedAt)
	user.ID = session.UserID
	return session, user, err
}

func (r *authRepository) UpdateSession(sessionID int, newRefresh string, expiresAt time.Time) error {
	_, err := r.db.Exec(
		`UPDATE sessions SET refresh_token = $1, expires_at = $2 WHERE id = $3`,
		newRefresh, expiresAt, sessionID,
	)
	return err
}

func (r *authRepository) DeleteSessionByID(sessionID int) error {
	_, err := r.db.Exec(`DELETE FROM sessions WHERE id = $1`, sessionID)
	return err
}

func (r *authRepository) DeleteSessionByToken(token string) error {
	_, err := r.db.Exec(`DELETE FROM sessions WHERE refresh_token = $1`, token)
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
