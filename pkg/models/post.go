package models

import (
	"time"

	socialpb "cacc/proto/socialpb"
)

type Post struct {
	ID         int       `json:"id"`
	Texto      string    `json:"texto"`
	Author     string    `json:"author"`
	UserID     int       `json:"user_id"`
	ParentID   *int      `json:"parent_id,omitempty"`
	Likes      int       `json:"likes"`
	ReplyCount int       `json:"reply_count"`
	CreatedAt  time.Time `json:"created_at"`
	Replies    []Post    `json:"replies,omitempty"`
}

// ToProto converts to protobuf for internal serialization/caching
func (p *Post) ToProto() *socialpb.Post {
	pb := &socialpb.Post{
		Id:         int32(p.ID),
		Texto:      p.Texto,
		Author:     p.Author,
		UserId:     int32(p.UserID),
		Likes:      int32(p.Likes),
		ReplyCount: int32(p.ReplyCount),
		CreatedAt:  p.CreatedAt.UnixMilli(),
	}
	if p.ParentID != nil {
		pb.ParentId = int32(*p.ParentID)
	}
	for i := range p.Replies {
		pb.Replies = append(pb.Replies, p.Replies[i].ToProto())
	}
	return pb
}

// PostFromProto converts from protobuf back to model
func PostFromProto(pb *socialpb.Post) Post {
	p := Post{
		ID:         int(pb.Id),
		Texto:      pb.Texto,
		Author:     pb.Author,
		UserID:     int(pb.UserId),
		Likes:      int(pb.Likes),
		ReplyCount: int(pb.ReplyCount),
		CreatedAt:  time.UnixMilli(pb.CreatedAt),
	}
	if pb.ParentId != 0 {
		pid := int(pb.ParentId)
		p.ParentID = &pid
	}
	for _, r := range pb.Replies {
		p.Replies = append(p.Replies, PostFromProto(r))
	}
	if p.Replies == nil {
		p.Replies = []Post{}
	}
	return p
}

type Profile struct {
	Username   string `json:"username"`
	TotalPosts int    `json:"total_posts"`
	TotalLikes int    `json:"total_likes"`
	Posts      []Post `json:"posts"`
}
