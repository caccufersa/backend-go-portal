package models

import "time"

type User struct {
	ID        int       `json:"id"`
	UUID      string    `json:"uuid"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AuthResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	User         User   `json:"user"`
	ExpiresIn    int    `json:"expires_in"`
}

type Session struct {
	ID           int       `json:"id"`
	UserID       int       `json:"user_id"`
	RefreshToken string    `json:"-"`
	UserAgent    string    `json:"user_agent"`
	IP           string    `json:"ip"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}
