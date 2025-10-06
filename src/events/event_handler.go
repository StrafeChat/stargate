package events

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

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
// BroadcastToMultipleUsers broadcasts an event to multiple users concurrently
func (h *EventHandler) BroadcastToMultipleUsers(userIDs []string, eventData []byte) {
	if len(userIDs) == 0 {
		return
	}

	log.Printf("Broadcasting event to %d users concurrently: %v", len(userIDs), userIDs)

	// Use a worker pool for concurrent broadcasting
	const maxWorkers = 20
	numWorkers := len(userIDs)
	if numWorkers > maxWorkers {
		numWorkers = maxWorkers
	}

	userChan := make(chan string, len(userIDs))
	var wg sync.WaitGroup

	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for userID := range userChan {
				h.Broadcast(userID, eventData)
			}
		}()
	}

	// Send user IDs to workers
	for _, userID := range userIDs {
		userChan <- userID
	}
	close(userChan)

	// Wait for all workers to complete
	wg.Wait()
	log.Printf("Completed broadcasting to %d users", len(userIDs))
}

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

	// Log all connected users for debugging
	Manager.mu.RLock()
	connectedUsers := make([]string, 0, len(Manager.connections))
	for uid := range Manager.connections {
		connectedUsers = append(connectedUsers, uid)
	}
	Manager.mu.RUnlock()
	log.Printf("Currently connected users: %v", connectedUsers)

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

	// Use goroutines for concurrent broadcasting to all handlers
	var wg sync.WaitGroup
	successCount := int32(0)
	failureCount := int32(0)

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
		wg.Add(1)
		go func(h *WebSocketHandler) {
			defer wg.Done()

			// Log details about each handler
			log.Printf("Sending event to handler for user %s", userID)

			// Try sending with the handler's configured encoder
			// First, encode the payload using the handler's encoder
			finalPayload, err := h.encoder.Encode(standardPayload)
			if err != nil {
				log.Printf("Error encoding event for user %s: %v", userID, err)
				atomic.AddInt32(&failureCount, 1)
				return
			}

			// Determine message type based on encoder type
			messageType := websocket.BinaryMessage
			if _, isJSON := h.encoder.(*format.JSONEncoder); isJSON {
				messageType = websocket.TextMessage
			}

			// Send the encoded payload
			if err := h.conn.WriteMessage(messageType, finalPayload); err != nil {
				log.Printf("Error broadcasting event to user %s: %v", userID, err)
				atomic.AddInt32(&failureCount, 1)
			} else {
				log.Printf("Successfully broadcast event to user %s", userID)
				atomic.AddInt32(&successCount, 1)
			}
		}(handler)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	log.Printf("Broadcast summary for user %s: %d successful, %d failed",
		userID, atomic.LoadInt32(&successCount), atomic.LoadInt32(&failureCount))
}

/*_ StartEventListener begins listening to Redis pub/sub events with enhanced logging _*/
func (h *EventHandler) StartEventListener() {
	log.Println("Starting Redis pub/sub event listener")

	pubsub := database.Rdb.Subscribe("RELATIONSHIP_EVENTS", "USER_EVENTS", "ROOM_EVENTS", "SPACE_EVENTS", "VOICE_EVENTS")
	defer pubsub.Close()

	// Create a worker pool for processing events concurrently
	const numWorkers = 10
	eventChan := make(chan []byte, 100) // Buffered channel for events

	// Start worker goroutines
	for i := 0; i < numWorkers; i++ {
		go h.eventWorker(eventChan)
	}

	ch := pubsub.Channel()
	for msg := range ch {
		log.Printf("Received Redis pub/sub message: Channel=%s, Payload=%s",
			msg.Channel, msg.Payload)

		payload := []byte(strings.TrimSpace(msg.Payload))

		if len(payload) == 0 {
			log.Printf("Received empty payload in pub/sub message")
			continue
		}

		// Send event to worker pool for concurrent processing
		select {
		case eventChan <- payload:
			// Event queued successfully
		default:
			log.Printf("Event queue full, processing synchronously")
			h.processEvent(payload)
		}
	}

	log.Println("Redis pub/sub event listener stopped")
}

// eventWorker processes events from the event channel
func (h *EventHandler) eventWorker(eventChan <-chan []byte) {
	for payload := range eventChan {
		h.processEvent(payload)
	}
}

// processEvent handles individual event processing
func (h *EventHandler) processEvent(payload []byte) {
	var rawEvent map[string]interface{}
	if err := json.Unmarshal(payload, &rawEvent); err != nil {
		log.Printf("Error unmarshaling event (payload: %s): %v", string(payload), err)
		return
	}

	eventType, ok := rawEvent["type"].(string)
	if !ok || eventType == "" {
		log.Printf("Received event with invalid or empty type: %+v", rawEvent)
		return
	}

	senderID, _ := rawEvent["sender_id"].(string)
	createdAtFloat, _ := rawEvent["created_at"].(float64)
	createdAt := int64(createdAtFloat)

	log.Printf("Processed event: Type=%s, SenderID=%s, CreatedAt=%d, FullEvent=%+v", eventType, senderID, createdAt, rawEvent)

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
		return
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
			return
		}
		roomID, ok := data["room_id"].(string)
		if !ok {
			log.Printf("Message delete event has no room_id")
			return
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
			return
		}

		// Get room members with VIEW_ROOM permission from repository
		userRepo := repository.GetUserRepository(database.Session)
		roomMembers, roomMembersErr := userRepo.GetRoomMembersWithPermissions(roomID, "VIEW_ROOM")
		if roomMembersErr != nil {
			log.Printf("Error getting room members with permissions: %v", roomMembersErr)
			return
		}

		// Broadcast to all room members concurrently
		h.BroadcastToMultipleUsers(roomMembers, wsPayloadBytes)

	case "MESSAGE_EDIT":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Message edit event has no data")
			return
		}
		roomID, ok := data["room_id"].(string)
		if !ok {
			log.Printf("Message edit event has no room_id")
			return
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
			return
		}

		// Get room members with VIEW_ROOMS permission from repository
		userRepo := repository.GetUserRepository(database.Session)
		roomMembers, roomMembersErr := userRepo.GetRoomMembersWithPermissions(roomID, "VIEW_ROOMS")
		if roomMembersErr != nil {
			log.Printf("Error getting room members with permissions: %v", roomMembersErr)
			return
		}

		// Broadcast to all room members concurrently
		h.BroadcastToMultipleUsers(roomMembers, wsPayloadBytes)

	case "PRESENCE_UPDATE":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Presence update event has no data")
			return
		}

		userID, ok := data["user_id"].(string)
		if !ok {
			log.Printf("Presence update event has no user_id")
			return
		}

		presence, ok := data["presence"].(map[string]interface{})
		if !ok {
			log.Printf("Presence update event has invalid presence data")
			return
		}

		status, ok := presence["status"].(string)
		if !ok {
			log.Printf("Presence update event has no status")
			return
		}

		var customStatus string
		if cs, ok := presence["custom_status"]; ok && cs != nil {
			customStatus, _ = cs.(string)
		}

		log.Printf("Broadcasting Presence Update Event: User=%s, Status=%s", userID, status)
		userRepo := repository.GetUserRepository(database.Session)
		if broadcastErr := Manager.BroadcastPresenceUpdate(userID, status, customStatus, userRepo); broadcastErr != nil {
			log.Printf("Error broadcasting presence update: %v", broadcastErr)
		}

	case "SPACE_CREATE":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Space create event has no data")
			return
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
			return
		}

		// Broadcast to the space creator
		h.Broadcast(senderID, wsPayloadBytes)

	case "SPACE_UPDATED":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Space update event has no data")
			return
		}
		// Extract space ID from the top-level event, not from data
		spaceID, _ := rawEvent["space_id"].(string)
		log.Printf("Broadcasting Space Update Event: Space=%s, Updated by=%s", spaceID, senderID)

		// Construct space update payload
		spacePayload := struct {
			Op string      `json:"op"`
			D  interface{} `json:"d"`
		}{
			Op: EventDispatch,
			D: map[string]interface{}{
				"type":     "spaceUpdate",
				"space_id": spaceID,
				"data":     data,
			},
		}

		// Marshal the space payload
		wsPayloadBytes, err = json.Marshal(spacePayload)
		if err != nil {
			log.Printf("Error marshaling space update payload: %v", err)
			return
		}

		// Get space members to broadcast the update
		userRepo := repository.GetUserRepository(database.Session)
		spaceMembers, spaceMembersErr := userRepo.GetSpaceMembers(spaceID)
		if spaceMembersErr != nil {
			log.Printf("Error getting space members for update: %v", spaceMembersErr)
			return
		}

		// Broadcast to all space members concurrently
		h.BroadcastToMultipleUsers(spaceMembers, wsPayloadBytes)

	case "ROOM_CREATE":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Room create event has no data")
			return
		}
		log.Printf("Broadcasting Room Create Event: Room=%s, Creator=%s", data["id"], senderID)

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
					"parent_id":       data["parent_id"],
					"last_message_id": data["last_message_id"],
					"icon":            data["icon"],
					"created_at":      data["created_at"],
					"updated_at":      data["updated_at"],
					"space_id":        data["space_id"],
				},
			},
		}

		// Marshal the room payload
		wsPayloadBytes, err = json.Marshal(roomPayload)
		if err != nil {
			log.Printf("Error marshaling room payload: %v", err)
			return
		}

		// Get space_id from event to broadcast to all space members
		spaceID, ok := rawEvent["space_id"].(string)
		if !ok {
			log.Printf("Room create event has no space_id")
			return
		}

		// Get space members to broadcast the room creation
		userRepo := repository.GetUserRepository(database.Session)
		spaceMembers, spaceMembersErr := userRepo.GetSpaceMembers(spaceID)
		if spaceMembersErr != nil {
			log.Printf("Error getting space members for room creation: %v", spaceMembersErr)
			return
		}

		// Broadcast to all space members
		for _, memberID := range spaceMembers {
			h.Broadcast(memberID, wsPayloadBytes)
		}

	case "ROOM_DELETE":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Room delete event has no data")
			return
		}
		log.Printf("Broadcasting Room Delete Event: Room=%s, Deleted by=%s", data["room_id"], data["deleted_by"])

		// Get room ID from event data
		roomID, ok := data["room_id"].(string)
		if !ok {
			log.Printf("Room delete event has no room_id")
			return
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
			return
		}

		userRepo := repository.GetUserRepository(database.Session)

		// Use room data from event payload instead of querying database
		var members []string

		// Check if room has space_id (space rooms)
		if spaceIDRaw, hasSpaceID := data["space_id"]; hasSpaceID && spaceIDRaw != nil {
			// Handle both string and numeric space_id
			var spaceID string
			switch v := spaceIDRaw.(type) {
			case string:
				spaceID = v
			case float64:
				spaceID = fmt.Sprintf("%.0f", v)
			case int64:
				spaceID = fmt.Sprintf("%d", v)
			default:
				spaceID = fmt.Sprintf("%v", v)
			}

			if spaceID != "" && spaceID != "0" {
				// Get space members for space rooms
				spaceMembers, err := userRepo.GetSpaceMembers(spaceID)
				if err != nil {
					log.Printf("Error getting space members for deletion: %v", err)
					return
				}
				members = spaceMembers
				log.Printf("Room %s belongs to space %s, broadcasting to %d space members", roomID, spaceID, len(members))
			} else {
				// No valid space_id, use recipients for DMs/group chats
				if recipientsRaw, hasRecipients := data["recipients"]; hasRecipients && recipientsRaw != nil {
					if recipientsList, ok := recipientsRaw.([]interface{}); ok {
						for _, recipient := range recipientsList {
							if userID, ok := recipient.(string); ok {
								members = append(members, userID)
							}
						}
					}
				}
				log.Printf("Room %s is a DM/group chat, broadcasting to %d recipients", roomID, len(members))
			}
		} else {
			// No space_id, use recipients for DMs/group chats
			if recipientsRaw, hasRecipients := data["recipients"]; hasRecipients && recipientsRaw != nil {
				if recipientsList, ok := recipientsRaw.([]interface{}); ok {
					for _, recipient := range recipientsList {
						if userID, ok := recipient.(string); ok {
							members = append(members, userID)
						}
					}
				}
			}
			log.Printf("Room %s is a DM/group chat, broadcasting to %d recipients", roomID, len(members))
		}

		if len(members) > 0 {
			// Broadcast to all members
			for _, memberID := range members {
				h.Broadcast(memberID, wsPayloadBytes)
			}
		} else {
			log.Printf("No members found to broadcast room deletion event for room %s", roomID)
		}

	case "ROOM_MEMBER_ADD":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Room member add event has no data")
			return
		}
		log.Printf("Broadcasting Room Member Add Event: Room=%s, User=%s, Added by=%s", data["room_id"], data["user_id"], data["added_by"])

		// Get room ID from event data
		roomID, ok := data["room_id"].(string)
		if !ok {
			log.Printf("Room member add event has no room_id")
			return
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
			return
		}

		// Get updated recipients from event data
		recipients, ok := data["recipients"].([]interface{})
		if !ok {
			log.Printf("Room member add event has invalid recipients data")
			return
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
			return
		}
		log.Printf("Broadcasting Room Member Remove Event: Room=%s, User=%s, Removed by=%s", data["room_id"], data["user_id"], data["removed_by"])

		// Get room ID from event data
		roomID, ok := data["room_id"].(string)
		if !ok {
			log.Printf("Room member remove event has no room_id")
			return
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
			return
		}

		// Get updated recipients from event data
		recipients, ok := data["recipients"].([]interface{})
		if !ok {
			log.Printf("Room member remove event has invalid recipients data")
			return
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
			return
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
			return
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
			return
		}

		// Get room members from repository
		userRepo := repository.GetUserRepository(database.Session)
		roomMembers, roomMembersErr := userRepo.GetRoomMembers(roomID)
		if roomMembersErr != nil {
			log.Printf("Error getting room members for icon update: %v", roomMembersErr)
			return
		}

		// Broadcast to all room members
		for _, memberID := range roomMembers {
			h.Broadcast(memberID, wsPayloadBytes)
		}

	case "ROOM_UPDATE":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Room update event has no data")
			return
		}
		log.Printf("Broadcasting Room Update Event: Room=%s, Updated by=%s", data["room_id"], data["updated_by"])

		// Get room ID from event data
		roomID, ok := data["room_id"].(string)
		if !ok {
			log.Printf("Room update event has no room_id")
			return
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
			return
		}

		// Get room type and space ID to determine broadcast strategy
		userRepo := repository.GetUserRepository(database.Session)

		// Get room details to check type
		var roomType int
		var spaceID *int64
		var recipients []string
		roomQuery := "SELECT type, space_id, recipients FROM rooms WHERE id = ?"
		if err := userRepo.GetSession().Query(roomQuery, roomID).Scan(&roomType, &spaceID, &recipients); err != nil {
			log.Printf("Error getting room details for update: %v", err)
			return
		}

		// For TextRooms (type 2) and VoiceRooms (type 3), broadcast to space members
		// For PMs (type 0) and Group PMs (type 1), broadcast to room members
		if roomType == 2 || roomType == 3 {
			// TextRoom or VoiceRoom - broadcast to space members
			if spaceID == nil {
				log.Printf("TextRoom/VoiceRoom %s has no space_id, skipping broadcast", roomID)
				return
			}

			spaceIDStr := strconv.FormatInt(*spaceID, 10)
			spaceMembers, err := userRepo.GetSpaceMembers(spaceIDStr)
			if err != nil {
				log.Printf("Error getting space members for room update: %v", err)
				return
			}

			log.Printf("Broadcasting room update to %d space members for TextRoom/VoiceRoom %s", len(spaceMembers), roomID)
			for _, memberID := range spaceMembers {
				h.Broadcast(memberID, wsPayloadBytes)
			}
		} else {
			// PM or Group PM - broadcast to room members
			log.Printf("Broadcasting room update to %d room members for PM/GroupPM %s", len(recipients), roomID)
			for _, memberID := range recipients {
				h.Broadcast(memberID, wsPayloadBytes)
			}
		}

	case "ROOM_POSITIONS_UPDATE":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Room positions update event has no data")
			return
		}
		log.Printf("Broadcasting Room Positions Update Event: Updated by=%s", data["updated_by"])

		// Get space ID from event data to get all space members
		spaceID, ok := data["space_id"].(string)
		if !ok {
			log.Printf("Room positions update event has no space_id")
			return
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
			return
		}

		// Get space members from repository
		userRepo := repository.GetUserRepository(database.Session)
		spaceMembers, err := userRepo.GetSpaceMembers(spaceID)
		if err != nil {
			log.Printf("Error getting space members for room positions update: %v", err)
			return
		}

		// Broadcast to all space members
		for _, memberID := range spaceMembers {
			h.Broadcast(memberID, wsPayloadBytes)
		}
	case "ROOM_OWNERSHIP_TRANSFER":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Room ownership transfer event has no data")
			return
		}
		log.Printf("Broadcasting Room Ownership Transfer Event: Room=%s, Old Owner=%s, New Owner=%s", data["room_id"], data["old_owner"], data["new_owner"])

		// Get room ID from event data
		roomID, ok := data["room_id"].(string)
		if !ok {
			log.Printf("Room ownership transfer event has no room_id")
			return
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
			return
		}

		// Get room members from repository
		userRepo := repository.GetUserRepository(database.Session)
		roomMembers, err := userRepo.GetRoomMembers(roomID)
		if err != nil {
			log.Printf("Error getting room members for ownership transfer: %v", err)
			return
		}

		// Broadcast to all room members
		for _, memberID := range roomMembers {
			h.Broadcast(memberID, wsPayloadBytes)
		}
	case "VOICE_START_RINGING":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Voice start ringing event has no data: %f", rawEvent)
		}

		log.Printf("Broadcasting Voice Ringing Event: Room=%s, caller=%s", data["room_id"], data["caller"])

		// Get room ID from event data
		roomID, ok := data["room_id"].(string)
		if !ok {
			log.Printf("Voice participant event has no room_id")
			return
		}

		callerID, ok := data["caller"].(string)
		if !ok {
			log.Printf("Voice participant event has no room_id")
			return
		}

		updateData := map[string]interface{}{ // TODO: add timestamp
			"room_id":    roomID,
			"caller":     callerID,
			"event_type": rawEvent["type"],
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
			return
		}

		// Get room members from repository
		userRepo := repository.GetUserRepository(database.Session)
		roomMembers, err := userRepo.GetRoomMembersWithPermissions(roomID, "VIEW_ROOMS")
		if err != nil {
			log.Printf("Error getting room members with permissions for voice event: %v", err)
			return
		}

		// Broadcast to all room members
		log.Printf("Broadcasting to %s", roomMembers)
		for _, memberID := range roomMembers {
			h.Broadcast(memberID, wsPayloadBytes)
		}
	case "VOICE_STOP_RINGING":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Voice stop ringing event has no data: %f", rawEvent)
		}

		log.Printf("Broadcasting Voice Ringing Event: Room=%s, caller=%s", data["room_id"], data["caller"])

		// Get room ID from event data
		roomID, ok := data["room_id"].(string)
		if !ok {
			log.Printf("Voice participant event has no room_id")
			return
		}

		callerID, ok := data["caller"].(string)
		if !ok {
			log.Printf("Voice participant event has no room_id")
			return
		}

		updateData := map[string]interface{}{ // TODO: add timestamp
			"room_id":    roomID,
			"caller":     callerID,
			"event_type": rawEvent["type"],
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
			return
		}

		// Get room members from repository
		userRepo := repository.GetUserRepository(database.Session)
		roomMembers, err := userRepo.GetRoomMembersWithPermissions(roomID, "VIEW_ROOMS")
		if err != nil {
			log.Printf("Error getting room members with permissions for voice event: %v", err)
			return
		}

		// Broadcast to all room members
		log.Printf("Broadcasting to %s", roomMembers)
		for _, memberID := range roomMembers {
			h.Broadcast(memberID, wsPayloadBytes)
		}
	case "VOICE_PARTICIPANT_JOIN", "VOICE_PARTICIPANT_LEAVE":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Voice participant event has no data")
			return
		}
		log.Printf("Broadcasting Voice Join/Leave Event: Room=%s, Participant=%s", data["room_id"], data["participant_id"])

		// Get room ID from event data
		roomID, ok := data["room_id"].(string)
		if !ok {
			log.Printf("Voice participant event has no room_id")
			return
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
			return
		}

		// Get room members from repository
		userRepo := repository.GetUserRepository(database.Session)
		roomMembers, err := userRepo.GetRoomMembersWithPermissions(roomID, "VIEW_ROOMS")
		if err != nil {
			log.Printf("Error getting room members with permissions for voice event: %v", err)
			return
		}

		// Broadcast to all room members
		log.Printf("Broadcasting to %s", roomMembers)
		for _, memberID := range roomMembers {
			h.Broadcast(memberID, wsPayloadBytes)
		}

	case "MESSAGE_CREATE":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Message create event has no data")
			return
		}
		log.Printf("Broadcasting Message Create Event: Room=%s, Sender=%s", data["room_id"], senderID)

		// Get room ID from event data
		roomID, ok := data["room_id"].(string)
		if !ok {
			log.Printf("Message event has no room_id")
			return
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
			return
		}

		// Get room members with VIEW_ROOMS permission from repository
		userRepo := repository.GetUserRepository(database.Session)
		roomMembers, roomMembersErr := userRepo.GetRoomMembersWithPermissions(roomID, "VIEW_ROOMS")
		if roomMembersErr != nil {
			log.Printf("Error getting room members with permissions: %v", roomMembersErr)
			return
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
				return
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
			return
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
			return
		}

		// Get room members with VIEW_ROOM permission from repository
		userRepo := repository.GetUserRepository(database.Session)
		roomMembers, err := userRepo.GetRoomMembersWithPermissions(roomID, "VIEW_ROOM")
		if err != nil {
			log.Printf("Error getting room members with permissions: %v", err)
			return
		}

		// Broadcast to all room members
		for _, memberID := range roomMembers {
			h.Broadcast(memberID, wsPayloadBytes)
		}

	case "SPACE_ROLE_CREATE":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Space role create event has no data")
			return
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
			return
		}

		// Get space ID from event data
		spaceID, ok := data["space_id"].(string)
		if !ok {
			log.Printf("Space role create event has no space_id")
			return
		}

		// Get space members from repository
		userRepo := repository.GetUserRepository(database.Session)
		spaceMembers, spaceMembersErr := userRepo.GetSpaceMembers(spaceID)
		if spaceMembersErr != nil {
			log.Printf("Error getting space members for role creation: %v", spaceMembersErr)
			return
		}

		// Broadcast to all space members
		for _, memberID := range spaceMembers {
			h.Broadcast(memberID, wsPayloadBytes)
		}

	case "SPACE_ROLE_UPDATE":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Space role update event has no data")
			return
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
			return
		}

		// Get space ID from event data
		spaceID, ok := data["space_id"].(string)
		if !ok {
			log.Printf("Space role update event has no space_id")
			return
		}

		// Get space members from repository
		userRepo := repository.GetUserRepository(database.Session)
		spaceMembers, spaceMembersErr := userRepo.GetSpaceMembers(spaceID)
		if spaceMembersErr != nil {
			log.Printf("Error getting space members for role update: %v", spaceMembersErr)
			return
		}

		// Broadcast to all space members
		for _, memberID := range spaceMembers {
			h.Broadcast(memberID, wsPayloadBytes)
		}

	case "SPACE_ROLE_DELETE":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Space role delete event has no data")
			return
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
			return
		}

		// Get space ID from event data
		spaceID, ok := data["space_id"].(string)
		if !ok {
			log.Printf("Space role delete event has no space_id")
			return
		}

		// Get space members from repository
		userRepo := repository.GetUserRepository(database.Session)
		spaceMembers, spaceMembersErr := userRepo.GetSpaceMembers(spaceID)
		if spaceMembersErr != nil {
			log.Printf("Error getting space members for role deletion: %v", spaceMembersErr)
			return
		}

		// Broadcast to all space members
		for _, memberID := range spaceMembers {
			h.Broadcast(memberID, wsPayloadBytes)
		}

	case "SPACE_MEMBER_ROLE_UPDATE":
		spaceID, ok := rawEvent["space_id"].(string)
		if !ok {
			log.Printf("Space member role update event has no space_id")
			return
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
			return
		}

		// Get space members from repository
		userRepo := repository.GetUserRepository(database.Session)
		spaceMembers, spaceMembersErr := userRepo.GetSpaceMembers(spaceID)
		if spaceMembersErr != nil {
			log.Printf("Error getting space members for member role update: %v", spaceMembersErr)
			return
		}

		// Broadcast to all space members
		for _, memberID := range spaceMembers {
			h.Broadcast(memberID, wsPayloadBytes)
		}

	case "SPACE_MEMBER_REMOVE":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Space member remove event has no data")
			return
		}
		log.Printf("Broadcasting Space Member Remove Event: Space=%s, Member=%s", data["space_id"], data["user_id"])

		// Construct space member remove payload
		memberPayload := struct {
			Op string      `json:"op"`
			D  interface{} `json:"d"`
		}{
			Op: EventDispatch,
			D: map[string]interface{}{
				"type": "SPACE_MEMBER_REMOVE",
				"data": data,
			},
		}

		// Marshal the member payload
		wsPayloadBytes, err = json.Marshal(memberPayload)
		if err != nil {
			log.Printf("Error marshaling space member remove payload: %v", err)
			return
		}

		// Get space ID from event data
		spaceID, ok := data["space_id"].(string)
		if !ok {
			log.Printf("Space member remove event has no space_id")
			return
		}

		// Get space members from repository
		userRepo := repository.GetUserRepository(database.Session)
		spaceMembers, spaceMembersErr := userRepo.GetSpaceMembers(spaceID)
		if spaceMembersErr != nil {
			log.Printf("Error getting space members for member remove: %v", spaceMembersErr)
			return
		}

		// Broadcast to all space members
		for _, memberID := range spaceMembers {
			h.Broadcast(memberID, wsPayloadBytes)
		}

	case "SPACE_MEMBER_ADD":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Space member add event has no data")
			return
		}
		log.Printf("Broadcasting Space Member Add Event: Space=%s, Member=%s", data["space_id"], data["user_id"])

		// Construct space member add payload
		memberPayload := struct {
			Op string      `json:"op"`
			D  interface{} `json:"d"`
		}{
			Op: EventDispatch,
			D: map[string]interface{}{
				"type": "SPACE_MEMBER_ADD",
				"data": data,
			},
		}

		// Marshal the member payload
		wsPayloadBytes, err = json.Marshal(memberPayload)
		if err != nil {
			log.Printf("Error marshaling space member add payload: %v", err)
			return
		}

		// Get space ID from event data
		spaceID, ok := data["space_id"].(string)
		if !ok {
			log.Printf("Space member add event has no space_id")
			return
		}

		// Get space members from repository
		userRepo := repository.GetUserRepository(database.Session)
		spaceMembers, spaceMembersErr := userRepo.GetSpaceMembers(spaceID)
		if spaceMembersErr != nil {
			log.Printf("Error getting space members for member add: %v", spaceMembersErr)
			return
		}

		// Broadcast to all space members
		for _, memberID := range spaceMembers {
			h.Broadcast(memberID, wsPayloadBytes)
		}

	case "ROOM_REACTION_ADD", "ROOM_REACTION_REMOVE", "REACTION_ADD", "REACTION_REMOVE":
		// Debug: Log the entire raw event structure
		log.Printf("Raw reaction event received: %+v", rawEvent)

		// For reaction events, the data is at the root level, not nested under "data"
		messageID, ok := rawEvent["message_id"].(string)
		if !ok {
			log.Printf("Reaction event has no message_id")
			return
		}

		roomID, ok := rawEvent["room_id"].(string)
		if !ok {
			log.Printf("Reaction event has no room_id")
			return
		}

		emoji, ok := rawEvent["emoji"].(string)
		if !ok {
			log.Printf("Reaction event has no emoji")
			return
		}

		count, _ := rawEvent["count"].(float64)
		users, _ := rawEvent["users"].([]interface{})

		// Convert users array to string array
		userStrings := make([]string, len(users))
		for i, user := range users {
			if userStr, ok := user.(string); ok {
				userStrings[i] = userStr
			}
		}

		log.Printf("Broadcasting Reaction Event: Type=%s, Room=%s, Message=%s, Emoji=%s, Count=%.0f, Users=%v",
			eventType, roomID, messageID, emoji, count, userStrings)

		// Construct reaction payload
		reactionPayload := struct {
			Op string      `json:"op"`
			D  interface{} `json:"d"`
		}{
			Op: EventMessage,
			D: map[string]interface{}{
				"event_type": eventType,
				"data": map[string]interface{}{
					"message_id": messageID,
					"room_id":    roomID,
					"emoji":      emoji,
					"count":      int(count),
					"users":      userStrings,
				},
			},
		}

		// Marshal the reaction payload
		wsPayloadBytes, err = json.Marshal(reactionPayload)
		if err != nil {
			log.Printf("Error marshaling reaction payload: %v", err)
			return
		}

		log.Printf("Reaction payload marshaled successfully: %s", string(wsPayloadBytes))

		// Get room members with VIEW_ROOMS permission from repository
		userRepo := repository.GetUserRepository(database.Session)
		roomMembers, roomMembersErr := userRepo.GetRoomMembersWithPermissions(roomID, "VIEW_ROOMS")
		if roomMembersErr != nil {
			log.Printf("Error getting room members with permissions: %v", roomMembersErr)
			return
		}

		log.Printf("Found %d room members with VIEW_ROOMS permission: %v", len(roomMembers), roomMembers)

		// Broadcast to all room members concurrently
		h.BroadcastToMultipleUsers(roomMembers, wsPayloadBytes)

	case "CUSTOM_EMOJI_CREATE":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Custom emoji create event has no data")
			return
		}
		spaceID, ok := data["space_id"].(string)
		if !ok {
			log.Printf("Custom emoji create event has no space_id")
			return
		}
		shortcode, _ := data["shortcode"].(string)
		log.Printf("Broadcasting Custom Emoji Create Event: Space=%s, Shortcode=%s, Creator=%s", spaceID, shortcode, senderID)

		// Construct custom emoji creation payload
		emojiPayload := struct {
			Op string      `json:"op"`
			D  interface{} `json:"d"`
		}{
			Op: EventDispatch,
			D: map[string]interface{}{
				"type": "CUSTOM_EMOJI_CREATE",
				"data": map[string]interface{}{
					"space_id":   spaceID,
					"shortcode":  data["shortcode"],
					"file_id":    data["file_id"],
					"name":       data["name"],
					"created_by": data["created_by"],
					"created_at": data["created_at"],
				},
			},
		}

		// Marshal the emoji payload
		wsPayloadBytes, err = json.Marshal(emojiPayload)
		if err != nil {
			log.Printf("Error marshaling custom emoji create payload: %v", err)
			return
		}

		// Get space members to broadcast the emoji creation
		userRepo := repository.GetUserRepository(database.Session)
		spaceMembers, spaceMembersErr := userRepo.GetSpaceMembers(spaceID)
		if spaceMembersErr != nil {
			log.Printf("Error getting space members for custom emoji create: %v", spaceMembersErr)
			return
		}

		// Broadcast to all space members concurrently
		h.BroadcastToMultipleUsers(spaceMembers, wsPayloadBytes)

	case "CUSTOM_EMOJI_DELETE":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Custom emoji delete event has no data")
			return
		}
		spaceID, ok := data["space_id"].(string)
		if !ok {
			log.Printf("Custom emoji delete event has no space_id")
			return
		}
		shortcode, _ := data["shortcode"].(string)
		log.Printf("Broadcasting Custom Emoji Delete Event: Space=%s, Shortcode=%s, Deleted by=%s", spaceID, shortcode, senderID)

		// Construct custom emoji deletion payload
		emojiPayload := struct {
			Op string      `json:"op"`
			D  interface{} `json:"d"`
		}{
			Op: EventDispatch,
			D: map[string]interface{}{
				"type": "CUSTOM_EMOJI_DELETE",
				"data": map[string]interface{}{
					"space_id":   spaceID,
					"shortcode":  shortcode,
					"deleted_by": senderID,
				},
			},
		}

		// Marshal the emoji payload
		wsPayloadBytes, err = json.Marshal(emojiPayload)
		if err != nil {
			log.Printf("Error marshaling custom emoji delete payload: %v", err)
			return
		}

		// Get space members to broadcast the emoji deletion
		userRepo := repository.GetUserRepository(database.Session)
		spaceMembers, spaceMembersErr := userRepo.GetSpaceMembers(spaceID)
		if spaceMembersErr != nil {
			log.Printf("Error getting space members for custom emoji delete: %v", spaceMembersErr)
			return
		}

		// Broadcast to all space members concurrently
		h.BroadcastToMultipleUsers(spaceMembers, wsPayloadBytes)

	case "CUSTOM_EMOJI_UPDATE":
		data, ok := rawEvent["data"].(map[string]interface{})
		if !ok {
			log.Printf("Custom emoji update event has no data")
			return
		}
		spaceID, ok := data["space_id"].(string)
		if !ok {
			log.Printf("Custom emoji update event has no space_id")
			return
		}
		shortcode, _ := data["shortcode"].(string)
		log.Printf("Broadcasting Custom Emoji Update Event: Space=%s, Shortcode=%s, Updated by=%s", spaceID, shortcode, senderID)

		// Construct custom emoji update payload
		emojiPayload := struct {
			Op string      `json:"op"`
			D  interface{} `json:"d"`
		}{
			Op: EventDispatch,
			D: map[string]interface{}{
				"type": "CUSTOM_EMOJI_UPDATE",
				"data": map[string]interface{}{
					"space_id":   spaceID,
					"shortcode":  data["shortcode"],
					"file_id":    data["file_id"],
					"name":       data["name"],
					"updated_by": senderID,
					"updated_at": data["updated_at"],
				},
			},
		}

		// Marshal the emoji payload
		wsPayloadBytes, err = json.Marshal(emojiPayload)
		if err != nil {
			log.Printf("Error marshaling custom emoji update payload: %v", err)
			return
		}

		// Get space members to broadcast the emoji update
		userRepo := repository.GetUserRepository(database.Session)
		spaceMembers, spaceMembersErr := userRepo.GetSpaceMembers(spaceID)
		if spaceMembersErr != nil {
			log.Printf("Error getting space members for custom emoji update: %v", spaceMembersErr)
			return
		}

		// Broadcast to all space members concurrently
		h.BroadcastToMultipleUsers(spaceMembers, wsPayloadBytes)

	default:
		log.Printf("Unknown event type: %s", eventType)
	}
}

var EventManager = NewEventHandler()
