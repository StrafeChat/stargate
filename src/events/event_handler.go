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

	pubsub := database.Rdb.Subscribe("RELATIONSHIP_EVENTS", "USER_EVENTS", "ROOM_EVENTS", "SPACE_EVENTS", "VOICE_EVENTS")
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

		var rawEvent map[string]interface{}
		if err := json.Unmarshal(payload, &rawEvent); err != nil {
			log.Printf("Error unmarshaling event (payload: %s): %v", string(payload), err)
			continue
		}

		eventType, ok := rawEvent["type"].(string)
		if !ok || eventType == "" {
			log.Printf("Received event with invalid or empty type: %+v", rawEvent)
			continue
		}

		senderID, _ := rawEvent["sender_id"].(string)
		createdAtFloat, _ := rawEvent["created_at"].(float64)
		createdAt := int64(createdAtFloat)

		log.Printf("Processed event: Type=%s, SenderID=%s, CreatedAt=%d", eventType, senderID, createdAt)

		var opCode string
		switch eventType {
		case "RELATIONSHIP_CREATE":
			opCode = EventRelationshipCreate
		case "RELATIONSHIP_ACCEPT":
			opCode = EventRelationshipAccept
		case "RELATIONSHIP_DELETE":
			opCode = EventRelationshipDelete
		default:
			opCode = EventDispatch
		}

		id, _ := rawEvent["id"].(string)
		recipientId, _ := rawEvent["recipient_id"].(string)
		eventTypeLower := strings.ToLower(strings.Replace(eventType, "RELATIONSHIP_", "relationship", 1))

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
				ID:          id,
				SenderID:    senderID,
				RecipientId: recipientId,
				CreatedAt:   createdAt,
				Type:        eventTypeLower,
			},
		}

		wsPayloadBytes, err := json.Marshal(wsPayload)
		if err != nil {
			log.Printf("Error marshaling WebSocket payload: %v", err)
			continue
		}

		switch eventType {
		case "RELATIONSHIP_CREATE":
			log.Printf("Broadcasting Relationship Create Event: Sender=%s, Recipient=%s",
				senderID, recipientId)

			h.Broadcast(recipientId, wsPayloadBytes)
			h.Broadcast(senderID, wsPayloadBytes)

		case "RELATIONSHIP_ACCEPT":
			log.Printf("Broadcasting Relationship Accept Event: Sender=%s", senderID)

			h.Broadcast(senderID, wsPayloadBytes)
			h.Broadcast(recipientId, wsPayloadBytes)

		case "RELATIONSHIP_DELETE":
			log.Printf("Broadcasting Relationship Delete Event: Sender=%s", senderID)

			h.Broadcast(senderID, wsPayloadBytes)
			h.Broadcast(recipientId, wsPayloadBytes)

		case "MESSAGE_DELETE":
			data, ok := rawEvent["data"].(map[string]interface{})
			if !ok {
				log.Printf("Message delete event has no data")
				continue
			}
			roomID, ok := data["room_id"].(string)
			if !ok {
				log.Printf("Message delete event has no room_id")
				continue
			}
			messageID, _ := data["id"].(string)
			log.Printf("Broadcasting Message Delete Event: Room=%s, Message=%s", roomID, messageID)

			// Construct message deletion payload
			messagePayload := struct {
				Op string      `json:"op"`
				D  interface{} `json:"d"`
			}{
				Op: EventMessage,
				D: map[string]interface{}{
					"event_type": "MESSAGE_DELETE",
					"data": map[string]interface{}{
						"id":      messageID,
						"room_id": roomID,
					},
				},
			}

			// Marshal the message payload
			wsPayloadBytes, err = json.Marshal(messagePayload)
			if err != nil {
				log.Printf("Error marshaling message deletion payload: %v", err)
				continue
			}

			// Get room members with SEND_MESSAGES permission from repository
			userRepo := repository.NewUserRepository(database.Session)
			roomMembers, roomMembersErr := userRepo.GetRoomMembersWithPermissions(roomID, "SEND_MESSAGES")
			if roomMembersErr != nil {
				log.Printf("Error getting room members with permissions: %v", roomMembersErr)
				continue
			}

			// Broadcast to all room members
			for _, memberID := range roomMembers {
				h.Broadcast(memberID, wsPayloadBytes)
			}

		case "MESSAGE_EDIT":
			data, ok := rawEvent["data"].(map[string]interface{})
			if !ok {
				log.Printf("Message edit event has no data")
				continue
			}
			roomID, ok := data["room_id"].(string)
			if !ok {
				log.Printf("Message edit event has no room_id")
				continue
			}
			messageID, _ := data["id"].(string)
			log.Printf("Broadcasting Message Edit Event: Room=%s, Message=%s", roomID, messageID)

			// Construct message edit payload
			messagePayload := struct {
				Op string      `json:"op"`
				D  interface{} `json:"d"`
			}{
				Op: EventMessage,
				D: map[string]interface{}{
					"event_type": "MESSAGE_EDIT",
					"data": map[string]interface{}{
						"id":               messageID,
						"room_id":          roomID,
						"content":          data["content"],
						"edited_at":        data["edited_at"],
						"author_id":        data["author_id"],
						"mention_everyone": data["mention_everyone"],
						"mention_roles":    data["mention_roles"],
						"mention_rooms":    data["mention_rooms"],
						"mentions":         data["mentions"],
					},
				},
			}

			// Marshal the message payload
			wsPayloadBytes, err = json.Marshal(messagePayload)
			if err != nil {
				log.Printf("Error marshaling message edit payload: %v", err)
				continue
			}

			// Get room members with SEND_MESSAGES permission from repository
			userRepo := repository.NewUserRepository(database.Session)
			roomMembers, roomMembersErr := userRepo.GetRoomMembersWithPermissions(roomID, "SEND_MESSAGES")
			if roomMembersErr != nil {
				log.Printf("Error getting room members with permissions: %v", roomMembersErr)
				continue
			}

			// Broadcast to all room members
			for _, memberID := range roomMembers {
				h.Broadcast(memberID, wsPayloadBytes)
			}

		case "PRESENCE_UPDATE":
			data, ok := rawEvent["data"].(map[string]interface{})
			if !ok {
				log.Printf("Presence update event has no data")
				continue
			}

			userID, ok := data["user_id"].(string)
			if !ok {
				log.Printf("Presence update event has no user_id")
				continue
			}

			presence, ok := data["presence"].(map[string]interface{})
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
			if broadcastErr := Manager.BroadcastPresenceUpdate(userID, status, customStatus, userRepo); broadcastErr != nil {
				log.Printf("Error broadcasting presence update: %v", broadcastErr)
			}

		case "SPACE_CREATE":
			data, ok := rawEvent["data"].(map[string]interface{})
			if !ok {
				log.Printf("Space create event has no data")
				continue
			}
			spaceID, _ := data["id"].(string)
			log.Printf("Broadcasting Space Create Event: Space=%s, Creator=%s", spaceID, senderID)

			// Construct space creation payload
			spacePayload := struct {
				Op string      `json:"op"`
				D  interface{} `json:"d"`
			}{
				Op: EventDispatch,
				D: map[string]interface{}{
					"type": "SPACE_CREATE",
					"data": data,
				},
			}

			// Marshal the space payload
			wsPayloadBytes, err = json.Marshal(spacePayload)
			if err != nil {
				log.Printf("Error marshaling space payload: %v", err)
				continue
			}

			// Broadcast to the space creator
			h.Broadcast(senderID, wsPayloadBytes)

		case "ROOM_CREATE":
			data, ok := rawEvent["data"].(map[string]interface{})
			if !ok {
				log.Printf("Room create event has no data")
				continue
			}
			log.Printf("Broadcasting Room Create Event: Room=%s, Creator=%s", data["room_id"], senderID)

			// Construct room creation payload
			roomPayload := struct {
				Op string      `json:"op"`
				D  interface{} `json:"d"`
			}{
				Op: EventDispatch,
				D: map[string]interface{}{
					"type": "ROOM_CREATE",
					"data": map[string]interface{}{
						"id":              data["id"],
						"name":            data["name"],
						"type":            data["type"],
						"recipients":      data["recipients"],
						"creator":         data["creator"],
						"last_message_id": data["last_message_id"],
						"icon":            data["icon"],
						"created_at":      data["created_at"],
						"updated_at":      data["updated_at"],
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
			recipients, ok := data["recipients"].([]interface{})
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

		case "ROOM_DELETE":
			data, ok := rawEvent["data"].(map[string]interface{})
			if !ok {
				log.Printf("Room delete event has no data")
				continue
			}
			log.Printf("Broadcasting Room Delete Event: Room=%s, Deleted by=%s", data["room_id"], data["deleted_by"])

			// Get room ID from event data
			roomID, ok := data["room_id"].(string)
			if !ok {
				log.Printf("Room delete event has no room_id")
				continue
			}

			// Construct room deletion payload
			roomPayload := struct {
				Op string      `json:"op"`
				D  interface{} `json:"d"`
			}{
				Op: EventDispatch,
				D: map[string]interface{}{
					"type": "ROOM_DELETE",
					"data": map[string]interface{}{
						"room_id":    roomID,
						"deleted_by": data["deleted_by"],
					},
				},
			}

			// Marshal the room payload
			wsPayloadBytes, err = json.Marshal(roomPayload)
			if err != nil {
				log.Printf("Error marshaling room deletion payload: %v", err)
				continue
			}

			// Get room members from repository before deletion
			userRepo := repository.NewUserRepository(database.Session)
			roomMembers, roomMembersErr := userRepo.GetRoomMembers(roomID)
			if roomMembersErr != nil {
				log.Printf("Error getting room members for deletion: %v", roomMembersErr)
				continue
			}

			// Broadcast to all room members
			for _, memberID := range roomMembers {
				h.Broadcast(memberID, wsPayloadBytes)
			}

		case "ROOM_MEMBER_ADD":
			data, ok := rawEvent["data"].(map[string]interface{})
			if !ok {
				log.Printf("Room member add event has no data")
				continue
			}
			log.Printf("Broadcasting Room Member Add Event: Room=%s, User=%s, Added by=%s", data["room_id"], data["user_id"], data["added_by"])

			// Get room ID from event data
			roomID, ok := data["room_id"].(string)
			if !ok {
				log.Printf("Room member add event has no room_id")
				continue
			}

			// Construct room member addition payload
			roomPayload := struct {
				Op string      `json:"op"`
				D  interface{} `json:"d"`
			}{
				Op: EventDispatch,
				D: map[string]interface{}{
					"type": "ROOM_MEMBER_ADD",
					"data": map[string]interface{}{
						"room_id":    roomID,
						"user_id":    data["user_id"],
						"added_by":   data["added_by"],
						"recipients": data["recipients"],
					},
				},
			}

			// Marshal the room payload
			wsPayloadBytes, err = json.Marshal(roomPayload)
			if err != nil {
				log.Printf("Error marshaling room member addition payload: %v", err)
				continue
			}

			// Get updated recipients from event data
			recipients, ok := data["recipients"].([]interface{})
			if !ok {
				log.Printf("Room member add event has invalid recipients data")
				continue
			}

			// Broadcast to all recipients (including the new member)
			for _, recipient := range recipients {
				if recipientID, ok := recipient.(string); ok {
					h.Broadcast(recipientID, wsPayloadBytes)
				}
			}

		case "ROOM_MEMBER_REMOVE":
			data, ok := rawEvent["data"].(map[string]interface{})
			if !ok {
				log.Printf("Room member remove event has no data")
				continue
			}
			log.Printf("Broadcasting Room Member Remove Event: Room=%s, User=%s, Removed by=%s", data["room_id"], data["user_id"], data["removed_by"])

			// Get room ID from event data
			roomID, ok := data["room_id"].(string)
			if !ok {
				log.Printf("Room member remove event has no room_id")
				continue
			}

			// Construct room member removal payload
			roomPayload := struct {
				Op string      `json:"op"`
				D  interface{} `json:"d"`
			}{
				Op: EventDispatch,
				D: map[string]interface{}{
					"type": "ROOM_MEMBER_REMOVE",
					"data": map[string]interface{}{
						"room_id":     roomID,
						"user_id":     data["user_id"],
						"removed_by":  data["removed_by"],
						"recipients":  data["recipients"],
						"new_creator": data["new_creator"],
					},
				},
			}

			// Marshal the room payload
			wsPayloadBytes, err = json.Marshal(roomPayload)
			if err != nil {
				log.Printf("Error marshaling room member removal payload: %v", err)
				continue
			}

			// Get updated recipients from event data
			recipients, ok := data["recipients"].([]interface{})
			if !ok {
				log.Printf("Room member remove event has invalid recipients data")
				continue
			}

			// Also notify the removed user
			removedUserID, ok := data["user_id"].(string)
			if ok {
				h.Broadcast(removedUserID, wsPayloadBytes)
			}

			// Broadcast to all remaining recipients
			for _, recipient := range recipients {
				if recipientID, ok := recipient.(string); ok {
					h.Broadcast(recipientID, wsPayloadBytes)
				}
			}

		case "ROOM_ICON_UPDATE":
			log.Printf("Processing Room Icon Update Event")

			// Get room ID - check root level first, then data
			var roomID string
			var ok bool
			roomID, ok = rawEvent["room_id"].(string)
			if !ok {
				if data, dok := rawEvent["data"].(map[string]interface{}); dok {
					roomID, ok = data["room_id"].(string)
				}
			}
			if !ok {
				log.Printf("Room icon update event has no room_id")
				continue
			}

			// Get icon URL - check root level first, then data
			var iconURL string
			iconURL, ok = rawEvent["icon_url"].(string)
			if !ok {
				if data, dok := rawEvent["data"].(map[string]interface{}); dok {
					iconURL, ok = data["icon_url"].(string)
				}
			}
			if !ok {
				log.Printf("Room icon update event has no icon_url")
				continue
			}

			log.Printf("Broadcasting Room Icon Update Event: Room=%s, Icon=%s", roomID, iconURL)

			// Construct room update payload with icon
			updateData := map[string]interface{}{
				"room_id":   roomID,
				"icon":      iconURL,
				"timestamp": createdAt,
			}

			roomPayload := struct {
				Op string      `json:"op"`
				D  interface{} `json:"d"`
			}{
				Op: EventDispatch,
				D: map[string]interface{}{
					"type": "ROOM_UPDATE",
					"data": updateData,
				},
			}

			// Marshal the room payload
			wsPayloadBytes, err = json.Marshal(roomPayload)
			if err != nil {
				log.Printf("Error marshaling room icon update payload: %v", err)
				continue
			}

			// Get room members from repository
			userRepo := repository.NewUserRepository(database.Session)
			roomMembers, roomMembersErr := userRepo.GetRoomMembers(roomID)
			if roomMembersErr != nil {
				log.Printf("Error getting room members for icon update: %v", roomMembersErr)
				continue
			}

			// Broadcast to all room members
			for _, memberID := range roomMembers {
				h.Broadcast(memberID, wsPayloadBytes)
			}

		case "ROOM_UPDATE":
			data, ok := rawEvent["data"].(map[string]interface{})
			if !ok {
				log.Printf("Room update event has no data")
				continue
			}
			log.Printf("Broadcasting Room Update Event: Room=%s, Updated by=%s", data["room_id"], data["updated_by"])

			// Get room ID from event data
			roomID, ok := data["room_id"].(string)
			if !ok {
				log.Printf("Room update event has no room_id")
				continue
			}

			// Construct room update payload
			updateData := map[string]interface{}{
				"room_id":    roomID,
				"updated_by": data["updated_by"],
				"timestamp":  data["timestamp"],
			}

			// Add updated fields if they exist
			if name, exists := data["name"]; exists {
				updateData["name"] = name
			}
			if topic, exists := data["topic"]; exists {
				updateData["topic"] = topic
			}
			if icon, exists := data["icon"]; exists {
				updateData["icon"] = icon
			}

			roomPayload := struct {
				Op string      `json:"op"`
				D  interface{} `json:"d"`
			}{
				Op: EventDispatch,
				D: map[string]interface{}{
					"type": "ROOM_UPDATE",
					"data": updateData,
				},
			}

			// Marshal the room payload
			wsPayloadBytes, err = json.Marshal(roomPayload)
			if err != nil {
				log.Printf("Error marshaling room update payload: %v", err)
				continue
			}

			// Get room members from repository
			userRepo := repository.NewUserRepository(database.Session)
			roomMembers, roomMembersErr := userRepo.GetRoomMembers(roomID)
			if roomMembersErr != nil {
				log.Printf("Error getting room members for update: %v", err)
				continue
			}

			// Broadcast to all room members
			for _, memberID := range roomMembers {
				h.Broadcast(memberID, wsPayloadBytes)
			}

		case "ROOM_POSITIONS_UPDATE":
			data, ok := rawEvent["data"].(map[string]interface{})
			if !ok {
				log.Printf("Room positions update event has no data")
				continue
			}
			log.Printf("Broadcasting Room Positions Update Event: Updated by=%s", data["updated_by"])

			// Get space ID from event data to get all space members
			spaceID, ok := data["space_id"].(string)
			if !ok {
				log.Printf("Room positions update event has no space_id")
				continue
			}

			// Construct room positions update payload
			updateData := map[string]interface{}{
				"room_positions": data["room_positions"],
				"updated_by":     data["updated_by"],
				"timestamp":      data["timestamp"],
				"space_id":       spaceID,
			}

			roomPayload := struct {
				Op string      `json:"op"`
				D  interface{} `json:"d"`
			}{
				Op: EventDispatch,
				D: map[string]interface{}{
					"type": "ROOM_POSITIONS_UPDATE",
					"data": updateData,
				},
			}

			// Marshal the room payload
			wsPayloadBytes, err = json.Marshal(roomPayload)
			if err != nil {
				log.Printf("Error marshaling room update payload: %v", err)
				continue
			}

			// Get space members from repository
			userRepo := repository.NewUserRepository(database.Session)
			spaceMembers, err := userRepo.GetSpaceMembers(spaceID)
			if err != nil {
				log.Printf("Error getting space members for room positions update: %v", err)
				continue
			}

			// Broadcast to all space members
			for _, memberID := range spaceMembers {
				h.Broadcast(memberID, wsPayloadBytes)
			}
		case "ROOM_OWNERSHIP_TRANSFER":
			data, ok := rawEvent["data"].(map[string]interface{})
			if !ok {
				log.Printf("Room ownership transfer event has no data")
				continue
			}
			log.Printf("Broadcasting Room Ownership Transfer Event: Room=%s, Old Owner=%s, New Owner=%s", data["room_id"], data["old_owner"], data["new_owner"])

			// Get room ID from event data
			roomID, ok := data["room_id"].(string)
			if !ok {
				log.Printf("Room ownership transfer event has no room_id")
				continue
			}

			// Construct room ownership transfer payload
			ownershipPayload := struct {
				Op string      `json:"op"`
				D  interface{} `json:"d"`
			}{
				Op: EventDispatch,
				D: map[string]interface{}{
					"type": "ROOM_OWNERSHIP_TRANSFER",
					"data": map[string]interface{}{
						"room_id":   roomID,
						"old_owner": data["old_owner"],
						"new_owner": data["new_owner"],
						"timestamp": data["timestamp"],
					},
				},
			}

			// Marshal the ownership transfer payload
			wsPayloadBytes, err = json.Marshal(ownershipPayload)
			if err != nil {
				log.Printf("Error marshaling room ownership transfer payload: %v", err)
				continue
			}

			// Get room members from repository
			userRepo := repository.NewUserRepository(database.Session)
			roomMembers, err := userRepo.GetRoomMembers(roomID)
			if err != nil {
				log.Printf("Error getting room members for ownership transfer: %v", err)
				continue
			}

			// Broadcast to all room members
			for _, memberID := range roomMembers {
				h.Broadcast(memberID, wsPayloadBytes)
			}

		case "VOICE_PARTICIPANT_JOIN", "VOICE_PARTICIPANT_LEAVE":
			data, ok := rawEvent["data"].(map[string]interface{})
			if !ok {
				log.Printf("Voice participant event has no data")
				continue
			}
			log.Printf("Broadcasting Voice Join/Leave Event: Room=%s, Participant=%s", data["room_id"], data["participant_id"])

			// Get room ID from event data
			roomID, ok := data["room_id"].(string)
			if !ok {
				log.Printf("Voice participant event has no room_id")
				continue
			}

			// Construct voice participant payload
			updateData := map[string]interface{}{
				"room_id":        roomID,
				"participant_id": data["participant_id"],
				"timestamp":      data["timestamp"],
				"event_type":     rawEvent["type"],
			}

			voicePayload := struct {
				Op string      `json:"op"`
				D  interface{} `json:"d"`
			}{
				Op: EventDispatch,
				D: map[string]interface{}{
					"type": rawEvent["type"],
					"data": updateData,
				},
			}

			// Marshal the voice payload
			wsPayloadBytes, err = json.Marshal(voicePayload)
			if err != nil {
				log.Printf("Error marshaling voice participant payload: %v", err)
				continue
			}

			// Get room members from repository
			userRepo := repository.NewUserRepository(database.Session)
			roomMembers, err := userRepo.GetRoomMembers(roomID)
			if err != nil {
				log.Printf("Error getting room members for voice event: %v", err)
				continue
			}

			// Broadcast to all room members
			for _, memberID := range roomMembers {
				h.Broadcast(memberID, wsPayloadBytes)
			}

		case "MESSAGE_CREATE":
			data, ok := rawEvent["data"].(map[string]interface{})
			if !ok {
				log.Printf("Message create event has no data")
				continue
			}
			log.Printf("Broadcasting Message Create Event: Room=%s, Sender=%s", data["room_id"], senderID)

			// Get room ID from event data
			roomID, ok := data["room_id"].(string)
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
						"id":                 data["id"],
						"content":            data["content"],
						"author_id":          data["author_id"],
						"room_id":            roomID,
						"created_at":         data["created_at"],
						"edited_at":          nil,
						"attachments":        data["attachments"],
						"message_references": data["message_references"],
						"type":               data["type"],
						"system":             data["system"],
						"system_type":        data["system_type"],
						"system_data":        data["system_data"],
						"mention_everyone":   data["mention_everyone"],
						"mention_roles":      data["mention_roles"],
						"mention_rooms":      data["mention_rooms"],
						"mentions":           data["mentions"],
					},
				},
			}

			// Marshal the message payload
			wsPayloadBytes, err = json.Marshal(messagePayload)
			if err != nil {
				log.Printf("Error marshaling message payload: %v", err)
				continue
			}

			// Get room members with SEND_MESSAGES permission from repository
			userRepo := repository.NewUserRepository(database.Session)
			roomMembers, roomMembersErr := userRepo.GetRoomMembersWithPermissions(roomID, "SEND_MESSAGES")
			if roomMembersErr != nil {
				log.Printf("Error getting room members with permissions: %v", roomMembersErr)
				continue
			}

			// Broadcast to all room members
			for _, memberID := range roomMembers {
				h.Broadcast(memberID, wsPayloadBytes)
			}

		case "TYPING_START":
			// Initial log before room_id extraction
			log.Printf("Processing typing indicator event from user %s", senderID)

			// Get room ID from event data
			var roomID string
			var ok bool

			// First check if room_id exists directly in the event payload (from Redis pub/sub)
			if roomID, ok = rawEvent["room_id"].(string); !ok {
				// Check if there's a room_id field at the root level of the event
				// The Redis payload format often has room_id at the root level for typing events
				var payloadMap map[string]interface{}
				if unmarshalErr := json.Unmarshal(payload, &payloadMap); unmarshalErr != nil {
					log.Printf("Error unmarshaling payload for typing event: %v", unmarshalErr)
					continue
				}
				roomIDField, exists := payloadMap["room_id"]
				if exists {
					if roomIDStr, isString := roomIDField.(string); isString {
						roomID = roomIDStr
						ok = true

						// Initialize data if it's nil
						if _, dok := rawEvent["data"]; !dok {
							rawEvent["data"] = make(map[string]interface{})
						}
						if dataMap, dok := rawEvent["data"].(map[string]interface{}); dok {
							dataMap["room_id"] = roomID
						}
					}
				}
			}

			if !ok {
				log.Printf("Typing indicator event has no room_id")
				continue
			}

			// Log the extracted room_id for debugging
			log.Printf("Broadcasting Typing Indicator Event: Room=%s, User=%s", roomID, senderID)

			// Construct typing indicator payload
			typingPayload := struct {
				Op string      `json:"op"`
				D  interface{} `json:"d"`
			}{
				Op: EventDispatch,
				D: map[string]interface{}{
					"type": "TYPING_INDICATOR",
					"data": map[string]interface{}{
						"room_id":    roomID,
						"user_id":    senderID,
						"created_at": createdAt,
					},
				},
			}

			// Marshal the typing payload
			wsPayloadBytes, err = json.Marshal(typingPayload)
			if err != nil {
				log.Printf("Error marshaling typing indicator payload: %v", err)
				continue
			}

			// Get room members with SEND_MESSAGES permission from repository
			userRepo := repository.NewUserRepository(database.Session)
			roomMembers, err := userRepo.GetRoomMembersWithPermissions(roomID, "SEND_MESSAGES")
			if err != nil {
				log.Printf("Error getting room members with permissions: %v", err)
				continue
			}

			// Broadcast to all room members
			for _, memberID := range roomMembers {
				h.Broadcast(memberID, wsPayloadBytes)
			}

		case "SPACE_ROLE_CREATE":
			data, ok := rawEvent["data"].(map[string]interface{})
			if !ok {
				log.Printf("Space role create event has no data")
				continue
			}
			log.Printf("Broadcasting Space Role Create Event: Space=%s, Role=%s, Creator=%s", data["space_id"], data["role_id"], senderID)

			// Construct space role creation payload
			rolePayload := struct {
				Op string      `json:"op"`
				D  interface{} `json:"d"`
			}{
				Op: EventDispatch,
				D: map[string]interface{}{
					"type": "SPACE_ROLE_CREATE",
					"data": data,
				},
			}

			// Marshal the role payload
			wsPayloadBytes, err = json.Marshal(rolePayload)
			if err != nil {
				log.Printf("Error marshaling space role creation payload: %v", err)
				continue
			}

			// Get space ID from event data
			spaceID, ok := data["space_id"].(string)
			if !ok {
				log.Printf("Space role create event has no space_id")
				continue
			}

			// Get space members from repository
			userRepo := repository.NewUserRepository(database.Session)
			spaceMembers, spaceMembersErr := userRepo.GetSpaceMembers(spaceID)
			if spaceMembersErr != nil {
				log.Printf("Error getting space members for role creation: %v", spaceMembersErr)
				continue
			}

			// Broadcast to all space members
			for _, memberID := range spaceMembers {
				h.Broadcast(memberID, wsPayloadBytes)
			}

		case "SPACE_ROLE_UPDATE":
			data, ok := rawEvent["data"].(map[string]interface{})
			if !ok {
				log.Printf("Space role update event has no data")
				continue
			}
			log.Printf("Broadcasting Space Role Update Event: Space=%s, Role=%s, Updated by=%s", data["space_id"], data["role_id"], senderID)

			// Construct space role update payload
			rolePayload := struct {
				Op string      `json:"op"`
				D  interface{} `json:"d"`
			}{
				Op: EventDispatch,
				D: map[string]interface{}{
					"type": "SPACE_ROLE_UPDATE",
					"data": data,
				},
			}

			// Marshal the role payload
			wsPayloadBytes, err = json.Marshal(rolePayload)
			if err != nil {
				log.Printf("Error marshaling space role update payload: %v", err)
				continue
			}

			// Get space ID from event data
			spaceID, ok := data["space_id"].(string)
			if !ok {
				log.Printf("Space role update event has no space_id")
				continue
			}

			// Get space members from repository
			userRepo := repository.NewUserRepository(database.Session)
			spaceMembers, spaceMembersErr := userRepo.GetSpaceMembers(spaceID)
			if spaceMembersErr != nil {
				log.Printf("Error getting space members for role update: %v", spaceMembersErr)
				continue
			}

			// Broadcast to all space members
			for _, memberID := range spaceMembers {
				h.Broadcast(memberID, wsPayloadBytes)
			}

		case "SPACE_ROLE_DELETE":
			data, ok := rawEvent["data"].(map[string]interface{})
			if !ok {
				log.Printf("Space role delete event has no data")
				continue
			}
			log.Printf("Broadcasting Space Role Delete Event: Space=%s, Role=%s, Deleted by=%s", data["space_id"], data["role_id"], senderID)

			// Construct space role deletion payload
			rolePayload := struct {
				Op string      `json:"op"`
				D  interface{} `json:"d"`
			}{
				Op: EventDispatch,
				D: map[string]interface{}{
					"type": "SPACE_ROLE_DELETE",
					"data": data,
				},
			}

			// Marshal the role payload
			wsPayloadBytes, err = json.Marshal(rolePayload)
			if err != nil {
				log.Printf("Error marshaling space role deletion payload: %v", err)
				continue
			}

			// Get space ID from event data
			spaceID, ok := data["space_id"].(string)
			if !ok {
				log.Printf("Space role delete event has no space_id")
				continue
			}

			// Get space members from repository
			userRepo := repository.NewUserRepository(database.Session)
			spaceMembers, spaceMembersErr := userRepo.GetSpaceMembers(spaceID)
			if spaceMembersErr != nil {
				log.Printf("Error getting space members for role deletion: %v", spaceMembersErr)
				continue
			}

			// Broadcast to all space members
			for _, memberID := range spaceMembers {
				h.Broadcast(memberID, wsPayloadBytes)
			}

		case "SPACE_MEMBER_ROLE_UPDATE":
			spaceID, ok := rawEvent["space_id"].(string)
			if !ok {
				log.Printf("Space member role update event has no space_id")
				continue
			}
			userID, _ := rawEvent["user_id"].(string)
			senderID, _ := rawEvent["sender_id"].(string)
			log.Printf("Broadcasting Space Member Role Update Event: Space=%s, Member=%s, Updated by=%s", spaceID, userID, senderID)

			// Construct space member role update payload
			innerPayload := map[string]interface{}{
				"type": "SPACE_MEMBER_ROLE_UPDATE",
				"data": rawEvent["data"],
			}

			// Marshal the inner payload
			wsPayloadBytes, err = json.Marshal(innerPayload)
			if err != nil {
				log.Printf("Error marshaling space member role update payload: %v", err)
				continue
			}

			// Get space members from repository
			userRepo := repository.NewUserRepository(database.Session)
			spaceMembers, spaceMembersErr := userRepo.GetSpaceMembers(spaceID)
			if spaceMembersErr != nil {
				log.Printf("Error getting space members for member role update: %v", spaceMembersErr)
				continue
			}

			// Broadcast to all space members
			for _, memberID := range spaceMembers {
				h.Broadcast(memberID, wsPayloadBytes)
			}

		default:
			log.Printf("Unknown event type: %s", eventType)
		}
	}

	log.Println("Redis pub/sub event listener stopped")
}

var EventManager = NewEventHandler()
