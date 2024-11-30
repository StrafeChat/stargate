package repository

import (
	"fmt"
	"time"

	"github.com/StrafeChat/stargate/src/models"
	"github.com/gocql/gocql"
)

type NotificationSubscription struct {
	UserID      string `json:"user_id"`
	Endpoint    string `json:"endpoint"`
	P256DH     string `json:"p256dh"`
	Auth       string `json:"auth"`
	ExpirationTime time.Time `json:"expiration_time"`
}

type NotificationRepository struct {
	session *gocql.Session
}

func NewNotificationRepository(session *gocql.Session) *NotificationRepository {
	return &NotificationRepository{session: session}
}

func (r *NotificationRepository) SaveSubscription(sub *models.PushSubscription) error {
	query := "INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, expiration_time) VALUES (?, ?, ?, ?, ?)"
	return r.session.Query(query, sub.UserID, sub.Endpoint, sub.P256DH, sub.Auth, sub.ExpirationTime).Exec()
}

func (r *NotificationRepository) GetSubscription(userID string) (*models.PushSubscription, error) {
	query := "SELECT user_id, endpoint, p256dh, auth, expiration_time FROM push_subscriptions WHERE user_id = ?"
	iter := r.session.Query(query, userID).Iter()
	
	var sub models.PushSubscription
	if iter.Scan(&sub.UserID, &sub.Endpoint, &sub.P256DH, &sub.Auth, &sub.ExpirationTime) {
		if err := iter.Close(); err != nil {
			return nil, fmt.Errorf("error fetching subscription: %v", err)
		}
		return &sub, nil
	}
	
	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("error fetching subscription: %v", err)
	}
	
	return nil, nil
}

func (r *NotificationRepository) RemoveSubscription(userID string) error {
	query := "DELETE FROM push_subscriptions WHERE user_id = ?"
	return r.session.Query(query, userID).Exec()
}
