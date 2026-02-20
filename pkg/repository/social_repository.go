package repository

import (
	"cacc/pkg/models"
	"database/sql"
	"fmt"
	"strings"
)

type SocialRepository interface {
	Feed(userID, limit, offset int) ([]models.Post, error)
	Thread(postID, userID int) (models.Post, error)
	Replies(parentID, userID, limit int) ([]models.Post, error)
	ProfilePosts(profileUserID, requestingUserID, limit int) ([]models.Post, error)
	ProfileStats(userID int) (totalPosts, totalLikes int)
	ProfileInfo(userID int) (username, displayName, bio string, err error)
	UpdateProfile(userID int, displayName, bio string) error
	CreatePost(texto, author string, userID int) (models.Post, error)
	CreateReply(texto, author string, userID, parentID int) (models.Post, error)
	IncrementReplyCount(parentID int) error
	LikePost(userID, postID int) (success bool, err error)
	UnlikePost(userID, postID int) (success bool, err error)
	IncLikeCount(postID int) (int, error)
	DecLikeCount(postID int) (int, error)
	GetLikeCount(postID int) (int, error)
	DeletePost(postID, userID int) (int, error)
	BatchLoadReplies(parentIDs []int, userID int) (map[int][]models.Post, error)
}

type socialRepository struct {
	db *sql.DB
}

func NewSocialRepository(db *sql.DB) SocialRepository {
	return &socialRepository{db: db}
}

func (r *socialRepository) Feed(userID, limit, offset int) ([]models.Post, error) {
	rows, err := r.db.Query(`
		SELECT p.id, p.texto, u.username, COALESCE(sp.display_name, u.username), p.user_id, p.likes, p.reply_count, p.created_at,
		       EXISTS(SELECT 1 FROM post_likes pl WHERE pl.post_id = p.id AND pl.user_id = $3) AS liked
		FROM posts p
		JOIN users u ON p.user_id = u.id
		LEFT JOIN social_profiles sp ON p.user_id = sp.user_id
		WHERE p.parent_id IS NULL
		ORDER BY p.created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []models.Post
	for rows.Next() {
		var p models.Post
		if err := rows.Scan(&p.ID, &p.Texto, &p.Author, &p.AuthorName, &p.UserID, &p.Likes, &p.ReplyCount, &p.CreatedAt, &p.Liked); err == nil {
			p.Replies = []models.Post{}
			posts = append(posts, p)
		}
	}
	return posts, nil
}

func (r *socialRepository) Thread(postID, userID int) (models.Post, error) {
	var p models.Post
	err := r.db.QueryRow(`
		SELECT p.id, p.texto, u.username, COALESCE(sp.display_name, u.username), p.user_id, p.parent_id, p.likes, p.reply_count, p.created_at,
		       EXISTS(SELECT 1 FROM post_likes pl WHERE pl.post_id = p.id AND pl.user_id = $2) AS liked
		FROM posts p
		JOIN users u ON p.user_id = u.id
		LEFT JOIN social_profiles sp ON p.user_id = sp.user_id
		WHERE p.id = $1
	`, postID, userID).Scan(
		&p.ID, &p.Texto, &p.Author, &p.AuthorName, &p.UserID, &p.ParentID, &p.Likes, &p.ReplyCount, &p.CreatedAt, &p.Liked,
	)
	return p, err
}

func (r *socialRepository) Replies(parentID, userID, limit int) ([]models.Post, error) {
	rows, err := r.db.Query(`
		SELECT p.id, p.texto, u.username, COALESCE(sp.display_name, u.username), p.user_id, p.likes, p.reply_count, p.created_at,
		       EXISTS(SELECT 1 FROM post_likes pl WHERE pl.post_id = p.id AND pl.user_id = $2) AS liked
		FROM posts p
		JOIN users u ON p.user_id = u.id
		LEFT JOIN social_profiles sp ON p.user_id = sp.user_id
		WHERE p.parent_id = $1
		ORDER BY p.created_at ASC
		LIMIT $3
	`, parentID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []models.Post
	for rows.Next() {
		var p models.Post
		if err := rows.Scan(&p.ID, &p.Texto, &p.Author, &p.AuthorName, &p.UserID, &p.Likes, &p.ReplyCount, &p.CreatedAt, &p.Liked); err == nil {
			posts = append(posts, p)
		}
	}
	return posts, nil
}

func (r *socialRepository) ProfilePosts(profileUserID, requestingUserID, limit int) ([]models.Post, error) {
	rows, err := r.db.Query(`
		SELECT p.id, p.texto, u.username, COALESCE(sp.display_name, u.username), p.user_id, p.parent_id, p.likes, p.reply_count, p.created_at,
		       EXISTS(SELECT 1 FROM post_likes pl WHERE pl.post_id = p.id AND pl.user_id = $2) AS liked
		FROM posts p
		JOIN users u ON p.user_id = u.id
		LEFT JOIN social_profiles sp ON p.user_id = sp.user_id
		WHERE p.user_id = $1
		ORDER BY p.created_at DESC
		LIMIT $3
	`, profileUserID, requestingUserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var posts []models.Post
	for rows.Next() {
		var p models.Post
		if err := rows.Scan(&p.ID, &p.Texto, &p.Author, &p.AuthorName, &p.UserID, &p.ParentID, &p.Likes, &p.ReplyCount, &p.CreatedAt, &p.Liked); err == nil {
			p.Replies = []models.Post{}
			posts = append(posts, p)
		}
	}
	return posts, nil
}

func (r *socialRepository) ProfileStats(userID int) (int, int) {
	var totalPosts, totalLikes int
	r.db.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(likes), 0)
		FROM posts WHERE user_id = $1
	`, userID).Scan(&totalPosts, &totalLikes)
	return totalPosts, totalLikes
}

func (r *socialRepository) ProfileInfo(userID int) (string, string, string, error) {
	var username, displayName, bio string
	err := r.db.QueryRow(`
		SELECT u.username, COALESCE(sp.display_name, u.username), COALESCE(sp.bio, '')
		FROM users u
		LEFT JOIN social_profiles sp ON u.id = sp.user_id
		WHERE u.id = $1
	`, userID).Scan(&username, &displayName, &bio)
	return username, displayName, bio, err
}

func (r *socialRepository) UpdateProfile(userID int, displayName, bio string) error {
	_, err := r.db.Exec(`
		INSERT INTO social_profiles (user_id, display_name, bio, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id) DO UPDATE 
		SET display_name = EXCLUDED.display_name, 
		    bio = EXCLUDED.bio,
		    updated_at = NOW()
	`, userID, displayName, bio)
	return err
}

func (r *socialRepository) CreatePost(texto, author string, userID int) (models.Post, error) {
	var p models.Post
	err := r.db.QueryRow(`
		INSERT INTO posts (texto, author, user_id, parent_id, likes, reply_count)
		VALUES ($1, $2, $3, NULL, 0, 0)
		RETURNING id, created_at
	`, texto, author, userID).Scan(&p.ID, &p.CreatedAt)
	return p, err
}

func (r *socialRepository) CreateReply(texto, author string, userID, parentID int) (models.Post, error) {
	var p models.Post
	err := r.db.QueryRow(`
		WITH new_reply AS (
			INSERT INTO posts (texto, author, user_id, parent_id, likes, reply_count)
			VALUES ($1, $2, $3, $4, 0, 0)
			RETURNING id, created_at
		)
		SELECT nr.id, nr.created_at FROM new_reply nr
	`, texto, author, userID, parentID).Scan(&p.ID, &p.CreatedAt)
	return p, err
}

func (r *socialRepository) IncrementReplyCount(parentID int) error {
	_, err := r.db.Exec(`UPDATE posts SET reply_count = reply_count + 1 WHERE id = $1`, parentID)
	return err
}

func (r *socialRepository) LikePost(userID, postID int) (bool, error) {
	var dummy int
	err := r.db.QueryRow(`
		INSERT INTO post_likes (user_id, post_id) VALUES ($1, $2)
		ON CONFLICT (user_id, post_id) DO NOTHING
		RETURNING 1
	`, userID, postID).Scan(&dummy)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (r *socialRepository) UnlikePost(userID, postID int) (bool, error) {
	var dummy int
	err := r.db.QueryRow(`
		DELETE FROM post_likes WHERE user_id = $1 AND post_id = $2
		RETURNING 1
	`, userID, postID).Scan(&dummy)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func (r *socialRepository) IncLikeCount(postID int) (int, error) {
	var likes int
	err := r.db.QueryRow(`
		UPDATE posts SET likes = likes + 1 WHERE id = $1
		RETURNING likes
	`, postID).Scan(&likes)
	return likes, err
}

func (r *socialRepository) DecLikeCount(postID int) (int, error) {
	var likes int
	err := r.db.QueryRow(`
		UPDATE posts SET likes = GREATEST(likes - 1, 0) WHERE id = $1
		RETURNING likes
	`, postID).Scan(&likes)
	return likes, err
}

func (r *socialRepository) GetLikeCount(postID int) (int, error) {
	var likes int
	err := r.db.QueryRow(`SELECT likes FROM posts WHERE id = $1`, postID).Scan(&likes)
	return likes, err
}

func (r *socialRepository) DeletePost(postID, userID int) (int, error) {
	var deletedID int
	err := r.db.QueryRow(`
		DELETE FROM posts WHERE id = $1 AND user_id = $2
		RETURNING id
	`, postID, userID).Scan(&deletedID)
	return deletedID, err
}

func (r *socialRepository) BatchLoadReplies(parentIDs []int, userID int) (map[int][]models.Post, error) {
	result := make(map[int][]models.Post, len(parentIDs))
	if len(parentIDs) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(parentIDs))
	args := make([]interface{}, len(parentIDs))
	for i, id := range parentIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT p.id, p.texto, u.username, COALESCE(sp.display_name, u.username), p.user_id, p.parent_id, p.likes, p.reply_count, p.created_at,
		       EXISTS(SELECT 1 FROM post_likes pl WHERE pl.post_id = p.id AND pl.user_id = $1) AS liked
		FROM posts p
		JOIN users u ON p.user_id = u.id
		LEFT JOIN social_profiles sp ON p.user_id = sp.user_id
		WHERE p.parent_id IN (%s)
		ORDER BY p.created_at ASC
	`, strings.Join(placeholders, ","))

	newArgs := make([]interface{}, len(args)+1)
	newArgs[0] = userID
	for i, v := range args {
		newArgs[i+1] = v
	}

	rows, err := r.db.Query(query, newArgs...)
	if err != nil {
		return result, err
	}
	defer rows.Close()

	for rows.Next() {
		var r models.Post
		var parentID int
		if err := rows.Scan(&r.ID, &r.Texto, &r.Author, &r.AuthorName, &r.UserID, &parentID, &r.Likes, &r.ReplyCount, &r.CreatedAt, &r.Liked); err == nil {
			r.ParentID = &parentID
			r.Replies = []models.Post{}
			result[parentID] = append(result[parentID], r)
		}
	}

	return result, nil
}
