package services

import (
	"fmt"
	"log"

	"github.com/StrafeChat/stargate/src/format"
	"github.com/StrafeChat/stargate/src/repository"
)

func HandleMessage(message format.Message, userId string, notificationRepo *repository.NotificationRepository) error {
	log.Printf("Received message: Type=%s, UserId=%s", message.Type, userId)

	switch message.Type {
	case "PING":
		return handlePing(message)
	case "NOTIFICATION":
		return handleNotification(message, userId, notificationRepo)
	default:
		log.Printf("Unhandled message type: %s", message.Type)
		return fmt.Errorf("unsupported message type: %s", message.Type)
	}
}

func handlePing(_ format.Message) error {
	log.Println("Received PING message")
	return nil
}

func handleNotification(_ format.Message, userId string, _ *repository.NotificationRepository) error {
	log.Printf("Handling notification for user %s", userId)
	
	// TODO: Implement actual notification handling logic
	return nil
}
