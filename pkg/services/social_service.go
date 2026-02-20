package services

import (
	"cacc/pkg/cache"
	"cacc/pkg/models"
	"cacc/pkg/repository"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type SocialService interface {
	Feed(limit, offset, userID int) ([]models.Post, error)
	Thread(postID, userID int) (models.Post, error)
	Profile(username string, profileUserID, requestingUserID int) (models.Profile, error)
	UpdateProfile(userID int, displayName, bio string) error
	CreatePost(texto, username string, userID int) (models.Post, error)
	CreateReply(texto, username string, userID, parentID int) (models.Post, error)
	Like(userID, postID int) (map[string]interface{}, error)
	Unlike(userID, postID int) (map[string]interface{}, error)
	Delete(userID, postID int) error
}

type socialService struct {
	repo  repository.SocialRepository
	auth  repository.AuthRepository // Used for finding users by username if needed for profile
	redis *cache.Redis
}

func NewSocialService(repo repository.SocialRepository, auth repository.AuthRepository, redis *cache.Redis) SocialService {
	return &socialService{repo: repo, auth: auth, redis: redis}
}

func (s *socialService) Feed(limit, offset, userID int) ([]models.Post, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}

	cacheKey := fmt.Sprintf("social:feed:%d:%d:lid%d", limit, offset, userID)
	var cached []models.Post
	if s.redis.Get(cacheKey, &cached) {
		return cached, nil
	}

	posts, err := s.repo.Feed(userID, limit, offset)
	if err != nil {
		return nil, err
	}

	if len(posts) > 0 {
		var postIDs []int
		for _, p := range posts {
			postIDs = append(postIDs, p.ID)
		}

		repliesMap, _ := s.repo.BatchLoadReplies(postIDs, userID)
		for i := range posts {
			if replies, ok := repliesMap[posts[i].ID]; ok {
				posts[i].Replies = replies
			}
		}
	}

	s.redis.Set(cacheKey, posts, 15*time.Second)
	return posts, nil
}

func (s *socialService) Thread(postID, userID int) (models.Post, error) {
	cacheKey := fmt.Sprintf("social:thread:%d:lid%d", postID, userID)
	var cached models.Post
	if s.redis.Get(cacheKey, &cached) {
		return cached, nil
	}

	p, err := s.repo.Thread(postID, userID)
	if err != nil {
		return models.Post{}, err
	}

	p.Replies = s.loadRepliesRecursive(p.ID, 5, userID)

	s.redis.Set(cacheKey, p, 30*time.Second)
	return p, nil
}

func (s *socialService) loadRepliesRecursive(parentID, maxDepth, userID int) []models.Post {
	if maxDepth <= 0 {
		return []models.Post{}
	}

	replies, err := s.repo.Replies(parentID, userID, 50)
	if err != nil {
		return []models.Post{}
	}

	for i := range replies {
		pid := parentID
		replies[i].ParentID = &pid
		if replies[i].ReplyCount > 0 {
			replies[i].Replies = s.loadRepliesRecursive(replies[i].ID, maxDepth-1, userID)
		} else {
			replies[i].Replies = []models.Post{}
		}
	}

	return replies
}

func (s *socialService) Profile(username string, profileUserID, requestingUserID int) (models.Profile, error) {
	userID := profileUserID

	if userID <= 0 && username != "" {
		userObj, _, err := s.auth.GetUserByUsername(strings.ToLower(username))
		if err != nil {
			return models.Profile{}, fmt.Errorf("usuário não encontrado")
		}
		userID = userObj.ID
	}

	if userID <= 0 {
		return models.Profile{}, fmt.Errorf("id ou username inválido")
	}

	cacheKey := fmt.Sprintf("social:profile:%d:lid%d", userID, requestingUserID)
	var cached models.Profile
	if s.redis.Get(cacheKey, &cached) {
		return cached, nil
	}

	totalPosts, totalLikes := s.repo.ProfileStats(userID)
	posts, err := s.repo.ProfilePosts(userID, requestingUserID, 100)
	if err != nil {
		return models.Profile{}, fmt.Errorf("erro ao buscar perfil")
	}

	if len(posts) > 0 {
		ids := make([]int, len(posts))
		for i, p := range posts {
			ids[i] = p.ID
		}
		repliesMap, _ := s.repo.BatchLoadReplies(ids, requestingUserID)
		for i := range posts {
			if replies, ok := repliesMap[posts[i].ID]; ok {
				posts[i].Replies = replies
			}
		}
	}

	un, displayName, bio, _ := s.repo.ProfileInfo(userID)

	profile := models.Profile{
		Username:    un,
		DisplayName: displayName,
		Bio:         bio,
		TotalPosts:  totalPosts,
		TotalLikes:  totalLikes,
		Posts:       posts,
	}

	s.redis.Set(cacheKey, profile, 30*time.Second)
	return profile, nil
}

func (s *socialService) UpdateProfile(userID int, displayName, bio string) error {
	err := s.repo.UpdateProfile(userID, displayName, bio)
	if err == nil {
		s.redis.Del(fmt.Sprintf("social:profile:%d", userID))
		s.redis.DelPattern("social:feed:*")
	}
	return err
}

func (s *socialService) CreatePost(texto, username string, userID int) (models.Post, error) {
	p, err := s.repo.CreatePost(texto, username, userID)
	if err != nil {
		return p, err
	}

	_, displayName, _, _ := s.repo.ProfileInfo(userID)

	p.Texto = texto
	p.Author = username
	p.AuthorName = username
	if displayName != "" {
		p.AuthorName = displayName
	}

	p.UserID = userID
	p.Likes = 0
	p.ReplyCount = 0
	p.Replies = []models.Post{}

	s.redis.DelPattern("social:feed:*")
	s.redis.Del(fmt.Sprintf("social:profile:%d", userID))

	return p, nil
}

func (s *socialService) CreateReply(texto, username string, userID, parentID int) (models.Post, error) {
	reply, err := s.repo.CreateReply(texto, username, userID, parentID)
	if err != nil {
		return reply, err
	}

	s.repo.IncrementReplyCount(parentID)
	_, displayName, _, _ := s.repo.ProfileInfo(userID)

	reply.Texto = texto
	reply.Author = username
	reply.AuthorName = username
	if displayName != "" {
		reply.AuthorName = displayName
	}

	reply.UserID = userID
	reply.ParentID = &parentID
	reply.Likes = 0
	reply.ReplyCount = 0
	reply.Replies = []models.Post{}

	s.redis.Del(fmt.Sprintf("social:thread:%d", parentID))
	s.redis.DelPattern("social:feed:*")
	s.redis.Del(fmt.Sprintf("social:profile:%d", userID))

	return reply, nil
}

func (s *socialService) toggleLike(userID, postID int, isLike bool) (map[string]interface{}, error) {
	var success bool
	var err error

	if isLike {
		success, err = s.repo.LikePost(userID, postID)
	} else {
		success, err = s.repo.UnlikePost(userID, postID)
	}

	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("erro interno na db")
	}

	var newLikes int
	if success {
		if isLike {
			newLikes, err = s.repo.IncLikeCount(postID)
		} else {
			newLikes, err = s.repo.DecLikeCount(postID)
		}
		if err != nil {
			return nil, fmt.Errorf("post não encontrado")
		}
	} else {
		newLikes, _ = s.repo.GetLikeCount(postID)
	}

	s.redis.Del(fmt.Sprintf("social:thread:%d", postID))
	s.redis.DelPattern("social:feed:*")

	return map[string]interface{}{"post_id": postID, "likes": newLikes}, nil
}

func (s *socialService) Like(userID, postID int) (map[string]interface{}, error) {
	return s.toggleLike(userID, postID, true)
}

func (s *socialService) Unlike(userID, postID int) (map[string]interface{}, error) {
	return s.toggleLike(userID, postID, false)
}

func (s *socialService) Delete(userID, postID int) error {
	_, err := s.repo.DeletePost(postID, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("post não encontrado ou sem permissão")
		}
		return err
	}

	s.redis.DelPattern("social:feed:*")
	s.redis.Del(fmt.Sprintf("social:thread:%d", postID))
	s.redis.Del(fmt.Sprintf("social:profile:%d", userID))

	return nil
}
