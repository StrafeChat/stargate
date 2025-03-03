package events

import (
	"encoding/json"
	"log"
	"strings"
	"sync"

	"github.com/StrafeChat/stargate/src/database"
	"github.com/StrafeChat/stargate/src/format"
	"github.com/StrafeChat/stargate/src/repository"
	"github.com/gorilla/websocket"
)

// Event represents a generic event structure for broadcasting
type Event struct {
	Type        string                 `json:"type"`
	ID          string                 `json:"id"`
	SenderID    string                 `json:"sender_id"`
	RecipientId string                 `json:"recipient_id"`
	CreatedAt   int64                  `json:"created_at"`
	Data        map[string]interface{} `json:"data"`
}

// EventHandler manages event broadcasting
type EventHandler struct {
	mu sync.RWMutex
}

// NewEventHandler creates a new EventHandler
func NewEventHandler() *EventHandler {
	return &EventHandler{}
}

// Broadcast sends an event to all subscribed handlers for a user
func (h *EventHandler) Broadcast(userID string, eventData []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	log.Printf("Attempting to broadcast event to user %s", userID)

	// Use ConnectionManager to get handlers for this user
	handlers, exists := Manager.connections[userID]
	if !exists {
		log.Printf("No WebSocket handlers found for user %s", userID)
		return
	}

	log.Printf("Found %d WebSocket handlers for user %s", len(handlers), userID)

	// Track successful and failed broadcasts
	successCount := 0
	failureCount := 0

	// Parse the original event to extract type and details
	var originalEvent map[string]interface{}
	if err := json.Unmarshal(eventData, &originalEvent); err != nil {
		log.Printf("Error parsing event data: %v", err)
		return
	}

	// Determine event type and op code
	eventType, ok := originalEvent["type"].(string)
	if !ok {
		eventType = "UNKNOWN"
	}

	// Standardize payload
	standardPayload := map[string]interface{}{
		"op": EventDispatch, // Default to DISPATCH
		"d":  originalEvent,
	}

	// Map specific event types to event names
	switch eventType {
	case "RELATIONSHIP_CREATE":
		standardPayload["op"] = EventRelationshipCreate
	case "RELATIONSHIP_ACCEPT":
		standardPayload["op"] = EventRelationshipAccept
	case "RELATIONSHIP_DELETE":
		standardPayload["op"] = EventRelationshipDelete
	case "MESSAGE":
		standardPayload["op"] = EventMessage
	}

	for handler := range handlers {
		// Log details about each handler
		log.Printf("Sending event to handler for user %s", userID)

		// Try sending with the handler's configured encoder
		// First, encode the payload using the handler's encoder
		finalPayload, err := handler.encoder.Encode(standardPayload)
		if err != nil {
			log.Printf("Error encoding event for user %s: %v", userID, err)
			failureCount++
			continue
		}

		// Determine message type based on encoder type
		messageType := websocket.BinaryMessage
		if _, isJSON := handler.encoder.(*format.JSONEncoder); isJSON {
			messageType = websocket.TextMessage
		}

		// Send the encoded payload
		if err := handler.conn.WriteMessage(messageType, finalPayload); err != nil {
			log.Printf("Error broadcasting event to user %s: %v", userID, err)
			failureCount++
		} else {
			log.Printf("Successfully broadcast event to user %s", userID)
			successCount++
		}
	}

	log.Printf("Broadcast summary for user %s: %d successful, %d failed",
		userID, successCount, failureCount)
}

/*_ StartEventListener begins listening to Redis pub/sub events with enhanced logging _*/
func (h *EventHandler) StartEventListener() {
	log.Println("Starting Redis pub/sub event listener")

	pubsub := database.Rdb.Subscribe("RELATIONSHIP_EVENTS", "USER_EVENTS", "ROOM_EVENTS")
	defer pubsub.Close()

	ch := pubsub.Channel()
	for msg := range ch {
		log.Printf("Received Redis pub/sub message: Channel=%s, Payload=%s",
			msg.Channel, msg.Payload)

		payload := []byte(strings.TrimSpace(msg.Payload))

		if len(payload) == 0 {
			log.Printf("Received empty payload in pub/sub message")
			continue
		}

		var event Event
		if err := json.Unmarshal(payload, &event); err != nil {
			log.Printf("Error unmarshaling event (payload: %s): %v", string(payload), err)
			continue
		}

		if event.Type == "" {
			log.Printf("Received event with empty type: %+v", event)
			continue
		}

		log.Printf("Processed event: Type=%s, SenderID=%s, CreatedAt=%d",
			event.Type, event.SenderID, event.CreatedAt)

		var opCode string
		switch event.Type {
		case "RELATIONSHIP_CREATE":
			opCode = EventRelationshipCreate
		case "RELATIONSHIP_ACCEPT":
			opCode = EventRelationshipAccept
		case "RELATIONSHIP_DELETE":
			opCode = EventRelationshipDelete
		default:
			opCode = EventDispatch
		}

		wsPayload := struct {
			Op string      `json:"op"`
			D  interface{} `json:"d"`
		}{
			Op: opCode,
			D: struct {
				ID          string `json:"id"`
				SenderID    string `json:"sender_id"`
				RecipientId string `json:"recipient_id"`
				CreatedAt   int64  `json:"created_at"`
				Type        string `json:"type"`
			}{
				ID:          event.ID,
				SenderID:    event.SenderID,
				RecipientId: event.RecipientId,
				CreatedAt:   event.CreatedAt,
				Type:        strings.ToLower(strings.Replace(event.Type, "RELATIONSHIP_", "relationship", 1)),
			},
		}

		wsPayloadBytes, err := json.Marshal(wsPayload)
		if err != nil {
			log.Printf("Error marshaling WebSocket payload: %v", err)
			continue
		}

		switch event.Type {
		case "RELATIONSHIP_CREATE":
			log.Printf("Broadcasting Relationship Create Event: Sender=%s, Recipient=%s",
				event.SenderID, event.RecipientId)

			h.Broadcast(event.RecipientId, wsPayloadBytes)
			h.Broadcast(event.SenderID, wsPayloadBytes)

		case "RELATIONSHIP_ACCEPT":
			log.Printf("Broadcasting Relationship Accept Event: Sender=%s", event.SenderID)

			h.Broadcast(event.SenderID, wsPayloadBytes)
			h.Broadcast(event.RecipientId, wsPayloadBytes)

		case "RELATIONSHIP_DELETE":
			log.Printf("Broadcasting Relationship Delete Event: Sender=%s", event.SenderID)

			h.Broadcast(event.SenderID, wsPayloadBytes)
			h.Broadcast(event.RecipientId, wsPayloadBytes)

		case "PRESENCE_UPDATE":
			if event.Data == nil {
				log.Printf("Presence update event has no data")
				continue
			}

			userID, ok := event.Data["user_id"].(string)
			if !ok {
				log.Printf("Presence update event has no user_id")
				continue
			}

			presence, ok := event.Data["presence"].(map[string]interface{})
			if !ok {
				log.Printf("Presence update event has invalid presence data")
				continue
			}

			status, ok := presence["status"].(string)
			if !ok {
				log.Printf("Presence update event has no status")
				continue
			}

			var customStatus string
			if cs, ok := presence["custom_status"]; ok && cs != nil {
				customStatus, _ = cs.(string)
			}

			log.Printf("Broadcasting Presence Update Event: User=%s, Status=%s", userID, status)
			userRepo := repository.NewUserRepository(database.Session)
			if err := Manager.BroadcastPresenceUpdate(userID, status, customStatus, userRepo); err != nil {
				log.Printf("Error broadcasting presence update: %v", err)
			}

		case "ROOM_CREATE":
			log.Printf("Broadcasting Room Create Event: Room=%s, Creator=%s", event.Data["room_id"], event.SenderID)

			// Construct room creation payload
			roomPayload := struct {
				Op string      `json:"op"`
				D  interface{} `json:"d"`
			}{
				Op: EventDispatch,
				D: map[string]interface{}{
					"type": "ROOM_CREATE",
					"data": map[string]interface{}{
						"id":              event.Data["id"],
						"name":            event.Data["name"],
						"type":            event.Data["type"],
						"recipients":      event.Data["recipients"],
						"creator":         event.Data["creator"],
						"last_message_id": event.Data["last_message_id"],
						"icon":            event.Data["icon"],
						"created_at":      event.Data["created_at"],
						"updated_at":      event.Data["updated_at"],
					},
				},
			}

			// Marshal the room payload
			wsPayloadBytes, err = json.Marshal(roomPayload)
			if err != nil {
				log.Printf("Error marshaling room payload: %v", err)
				continue
			}

			// Get recipients from event data
			recipients, ok := event.Data["recipients"].([]interface{})
			if !ok {
				log.Printf("Room event has invalid recipients data")
				continue
			}

			// Broadcast to all recipients
			for _, recipient := range recipients {
				if recipientID, ok := recipient.(string); ok {
					h.Broadcast(recipientID, wsPayloadBytes)
				}
			}

		case "MESSAGE_CREATE":
			log.Printf("Broadcasting Message Create Event: Room=%s, Sender=%s", event.Data["room_id"], event.SenderID)

			// Get room ID from event data
			roomID, ok := event.Data["room_id"].(string)
			if !ok {
				log.Printf("Message event has no room_id")
				continue
			}

			// Construct message payload
			messagePayload := struct {
				Op string      `json:"op"`
				D  interface{} `json:"d"`
			}{
				Op: EventMessage,
				D: map[string]interface{}{
					"type": "MESSAGE_CREATE",
					"data": map[string]interface{}{
						"id":          event.Data["id"],
						"content":     event.Data["content"],
						"author_id":   event.Data["author_id"],
						"room_id":     roomID,
						"created_at":  event.Data["created_at"],
						"edited_at":   nil,
						"attachments": event.Data["attachments"],
					},
				},
			}

			// Marshal the message payload
			wsPayloadBytes, err = json.Marshal(messagePayload)
			if err != nil {
				log.Printf("Error marshaling message payload: %v", err)
				continue
			}

			// Get room members from repository
			userRepo := repository.NewUserRepository(database.Session)
			roomMembers, err := userRepo.GetRoomMembers(roomID)
			if err != nil {
				log.Printf("Error getting room members: %v", err)
				continue
			}

			// Broadcast to all room members
			for _, memberID := range roomMembers {
				h.Broadcast(memberID, wsPayloadBytes)
			}

		default:
			log.Printf("Unknown event type: %s", event.Type)
		}
	}

	log.Println("Redis pub/sub event listener stopped")
}

var EventManager = NewEventHandler()
