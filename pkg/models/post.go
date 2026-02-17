package models

import "time"

type Post struct {
	ID        int       `json:"id"`
	Texto     string    `json:"texto"`
	Author    string    `json:"author"`
	ParentID  *int      `json:"parent_id,omitempty"`
	Likes     int       `json:"likes"`
	CreatedAt time.Time `json:"created_at"`
	Replies   []Post    `json:"replies,omitempty"`
}
