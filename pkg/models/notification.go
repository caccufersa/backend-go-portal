package models

import "time"

type Notification struct {
	ID          int       `json:"id"`
	UserID      int       `json:"user_id"`
	ActorID     *int      `json:"actor_id,omitempty"`
	ActorName   string    `json:"actor_name,omitempty"`
	ActorAvatar string    `json:"actor_avatar,omitempty"`
	Type        string    `json:"type"` // "like", "reply", "mention", "repost"
	PostID      *int      `json:"post_id,omitempty"`
	IsRead      bool      `json:"is_read"`
	CreatedAt   time.Time `json:"created_at"`
}
