package services

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/StrafeChat/stargate/src/models"
	"github.com/StrafeChat/stargate/src/repository"
)

type NotificationService struct {
	repo *repository.NotificationRepository
}

func NewNotificationService(repo *repository.NotificationRepository) *NotificationService {
	return &NotificationService{
		repo: repo,
	}
}

func (s *NotificationService) SaveSubscription(sub *models.PushSubscription) error {
	return s.repo.SaveSubscription(sub)
}

func (s *NotificationService) RemoveSubscription(userID string) error {
	return s.repo.RemoveSubscription(userID)
}

func (s *NotificationService) SendNotification(userID string, payload map[string]interface{}) error {
	sub, err := s.repo.GetSubscription(userID)
	if err != nil {
		return fmt.Errorf("failed to get subscription: %v", err)
	}

	vapidPrivateKey := os.Getenv("VAPID_PRIVATE_KEY")
	vapidPublicKey := os.Getenv("VAPID_PUBLIC_KEY")
	vapidContact := os.Getenv("VAPID_CONTACT_EMAIL")

	if vapidPrivateKey == "" || vapidPublicKey == "" || vapidContact == "" {
		return fmt.Errorf("missing required VAPID configuration")
	}

	if !strings.Contains(vapidContact, "@") {
		vapidContact = "mailto:" + vapidContact
	}

	subscription := &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.P256DH,
			Auth:   sub.Auth,
		},
	}

	options := &webpush.Options{
		VAPIDPrivateKey: vapidPrivateKey,
		VAPIDPublicKey:  vapidPublicKey,
		Subscriber:      vapidContact,
	}

	message, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %v", err)
	}

	resp, err := webpush.SendNotification(message, subscription, options)
	if err != nil {
		return fmt.Errorf("failed to send notification: %v", err)
	}
	defer resp.Body.Close()

	return nil
}
