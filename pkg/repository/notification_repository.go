package repository

import (
	"cacc/pkg/models"
	"database/sql"
)

type NotificationRepository interface {
	CreateNotification(userID int, actorID *int, notificationType string, postID *int) error
	GetNotifications(userID, limit, offset int) ([]models.Notification, error)
	MarkAsRead(userID int) error
}

type notificationRepository struct {
	db *sql.DB
}

func NewNotificationRepository(db *sql.DB) NotificationRepository {
	return &notificationRepository{db: db}
}

func (r *notificationRepository) CreateNotification(userID int, actorID *int, notificationType string, postID *int) error {
	// Don't notify yourself
	if actorID != nil && *actorID == userID {
		return nil
	}

	_, err := r.db.Exec(`
		INSERT INTO notifications (user_id, actor_id, type, post_id, is_read)
		VALUES ($1, $2, $3, $4, false)
	`, userID, actorID, notificationType, postID)
	return err
}

func (r *notificationRepository) GetNotifications(userID, limit, offset int) ([]models.Notification, error) {
	rows, err := r.db.Query(`
		SELECT n.id, n.user_id, n.actor_id, n.type, n.post_id, n.is_read, n.created_at,
		       COALESCE(u.username, ''), COALESCE(sp.avatar_url, '')
		FROM notifications n
		LEFT JOIN users u ON n.actor_id = u.id
		LEFT JOIN social_profiles sp ON n.actor_id = sp.user_id
		WHERE n.user_id = $1
		ORDER BY n.created_at DESC
		LIMIT $2 OFFSET $3
	`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notifs []models.Notification
	for rows.Next() {
		var n models.Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.ActorID, &n.Type, &n.PostID, &n.IsRead, &n.CreatedAt, &n.ActorName, &n.ActorAvatar); err == nil {
			notifs = append(notifs, n)
		}
	}
	return notifs, nil
}

func (r *notificationRepository) MarkAsRead(userID int) error {
	_, err := r.db.Exec(`UPDATE notifications SET is_read = true WHERE user_id = $1 AND is_read = false`, userID)
	return err
}
