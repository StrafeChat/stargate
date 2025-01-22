package events

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/StrafeChat/stargate/src/database"
	"github.com/StrafeChat/stargate/src/format"
	"github.com/StrafeChat/stargate/src/repository"
	"github.com/StrafeChat/stargate/src/services"
	"github.com/gorilla/websocket"
)

type WebSocketHandler struct {
	conn              *websocket.Conn
	encoder          format.Encoder
	userRepo         *repository.UserRepository
	notificationSvc  *services.NotificationService
	userID           string
	sessionToken     string
	format           format.Format
}

// WebSocket Event Types
const (
	EventDispatch           = "DISPATCH"
	EventHeartbeat         = "HEARTBEAT"
	EventIdentify          = "IDENTIFY"
	EventReady            = "READY"
	EventHeartbeatAck     = "HEARTBEAT_ACK"
	EventMessage          = "MESSAGE"
	EventRelationshipCreate = "RELATIONSHIP_CREATE"
	EventRelationshipAccept = "RELATIONSHIP_ACCEPT"
	EventRelationshipDelete = "RELATIONSHIP_DELETE"
)

// Standardized event payload structure
type EventPayload struct {
	Op string      `json:"op"`
	D  interface{} `json:"d"`
}

func NewWebSocketHandler(conn *websocket.Conn, r *http.Request) *WebSocketHandler {
	// Default to MessagePack
	selectedFormat := format.FormatMsgPack
	
	// Explicitly check query parameter
	formatParam := r.URL.Query().Get("format")
	log.Printf("Received WebSocket connection with format parameter: %q", formatParam)
	
	// Determine format based on query parameter
	switch formatParam {
	case "json":
		selectedFormat = format.FormatJSON
	case "msgpack", "":
		selectedFormat = format.FormatMsgPack
	default:
		log.Printf("Unknown format parameter: %q, defaulting to MessagePack", formatParam)
	}

	encoder, err := format.GetEncoder(string(selectedFormat))
	if err != nil {
		log.Printf("Error getting encoder for %s: %v, falling back to MessagePack", selectedFormat, err)
		encoder, _ = format.GetEncoder(string(format.FormatMsgPack))
		selectedFormat = format.FormatMsgPack
	}

	log.Printf("Initializing WebSocket handler with format: %s", selectedFormat)

	return &WebSocketHandler{
		conn:             conn,
		encoder:         encoder,
		userRepo:        repository.NewUserRepository(database.GetSession()),
		notificationSvc: services.NewNotificationService(nil),
		format:          selectedFormat,
	}
}

func (h *WebSocketHandler) HandlePayload(messageType int, payload []byte) error {
	log.Printf("Received payload: messageType=%d, length=%d, hex=%x", messageType, len(payload), payload)
	
	// Try decoding with multiple formats
	var base BasePayload
	var err error
	var successfulFormat string

	// List of encoders to try
	encoders := []struct{
		name string
		encoder format.Encoder
	}{
		{"Primary", h.encoder},
		{"JSON", &format.JSONEncoder{}},
		{"MessagePack", &format.MsgPackEncoder{}},
	}

	for _, enc := range encoders {
		err = enc.encoder.Decode(payload, &base)
		if err == nil {
			successfulFormat = enc.name
			break
		} else {
			log.Printf("Decoding with %s failed: %v", enc.name, err)
		}
	}

	// If still can't decode, log detailed error
	if err != nil {
		log.Printf("Failed to decode payload in any format. Raw payload (hex): %x", payload)
		return fmt.Errorf("failed to decode payload: %v", err)
	}

	log.Printf("Successfully decoded payload with %s format. Payload type: %s", successfulFormat, base.Type)

	switch base.Type {
	case PayloadTypeIdentify:
		return h.handleIdentify(payload)
	case PayloadTypeHeartbeat:
		if h.userID == "" {
			return fmt.Errorf("not authenticated")
		}
		return h.handleHeartbeat(payload)
	case PayloadTypeMessage:
		if h.userID == "" {
			return fmt.Errorf("not authenticated")
		}
		return h.handleMessage(payload)
	default:
		return fmt.Errorf("unknown payload type: %s", base.Type)
	}
}

func (h *WebSocketHandler) handleIdentify(payload []byte) error {
	var identifyPayload IdentifyPayload
	var err error

	// Try multiple encoders
	encoders := []struct{
		name string
		encoder format.Encoder
	}{
		{"Primary", h.encoder},
		{"JSON", &format.JSONEncoder{}},
		{"MessagePack", &format.MsgPackEncoder{}},
	}

	for _, enc := range encoders {
		err = enc.encoder.Decode(payload, &identifyPayload)
		if err == nil {
			log.Printf("Successfully decoded identify payload with %s", enc.name)
			break
		} else {
			log.Printf("Decoding identify payload with %s failed: %v", enc.name, err)
		}
	}

	if err != nil {
		log.Printf("Failed to decode identify payload. Raw payload (hex): %x", payload)
		return fmt.Errorf("failed to decode identify payload: %v", err)
	}

	// Validate session token
	userID, err := h.userRepo.ValidateSessionToken(identifyPayload.Token)
	if err != nil {
		return fmt.Errorf("authentication failed: %v", err)
	}

	h.userID = userID
	h.sessionToken = identifyPayload.Token

	// Add connection to ConnectionManager
	log.Printf("Adding WebSocket connection for user %s", userID)
	Manager.AddConnection(userID, h)

	// Optional: set user online
	if err := h.userRepo.SetUserOnline(userID); err != nil {
		log.Printf("Failed to set user online: %v", err)
	}

	// Get user details
	details, err := h.userRepo.GetUserDetails(userID)
	if err != nil {
		log.Printf("Failed to get user details: %v", err)
		details = repository.UserDetails{ID: userID}
	}

	// If user's status is not offline, broadcast presence update
	// if details.Presence.Status != "offline" {
		log.Printf("Broadcasting presence update for user %s", userID)
		if err := Manager.BroadcastPresenceUpdate(userID, details.Presence.Status, details.Presence.CustomStatus, h.userRepo); err != nil {
			log.Printf("Failed to broadcast presence update: %v", err)
		}
	// }

	// Get user relationships and requests
	relationships, err := h.userRepo.GetUserRelationships(userID)
	if err != nil {
		log.Printf("Failed to get user relationships: %v", err)
		relationships = []string{}
	}
	log.Printf("[WebSocket:READY] Got relationships for user %s: %v", userID, relationships)

	relationshipRequests, err := h.userRepo.GetUserRelationshipRequests(userID)
	if err != nil {
		log.Printf("Failed to get user relationship requests: %v", err)
		relationshipRequests = []repository.Relationship{}
	}
	log.Printf("[WebSocket:READY] Got relationship requests for user %s: %+v", userID, relationshipRequests)

	// Get related user IDs
	relatedUserIDs, err := h.userRepo.GetRelatedUserIDs(userID)
	if err != nil {
		log.Printf("Failed to get related user IDs: %v", err)
		relatedUserIDs = []string{}
	}

	// Add friend IDs to the list of users to fetch if not already included
	userIDsToFetch := make(map[string]bool)
	for _, id := range relatedUserIDs {
		userIDsToFetch[id] = true
	}
	for _, id := range relationships {
		userIDsToFetch[id] = true
	}

	// Convert map keys back to slice
	uniqueUserIDs := make([]string, 0, len(userIDsToFetch))
	for id := range userIDsToFetch {
		uniqueUserIDs = append(uniqueUserIDs, id)
	}

	// Get details for all related users and friends
	relatedUsers, err := h.userRepo.GetUsersDetails(uniqueUserIDs)
	if err != nil {
		log.Printf("Failed to get related users details: %v", err)
		relatedUsers = make(map[string]repository.UserDetails)
	}

	// Create standardized ready payload
	readyPayload := EventPayload{
		Op: EventReady,
		D: map[string]interface{}{
			"client_user":   details,
			"users":         relatedUsers,
			"relationships": relationships,
			"relationship_requests": relationshipRequests,
		},
	}

	return h.sendResponse(readyPayload)
}

func (h *WebSocketHandler) handleHeartbeat(payload []byte) error {
	var heartbeat HeartbeatPayload
	var err error

	// Try multiple encoders
	encoders := []struct{
		name string
		encoder format.Encoder
	}{
		{"Primary", h.encoder},
		{"JSON", &format.JSONEncoder{}},
		{"MessagePack", &format.MsgPackEncoder{}},
	}

	for _, enc := range encoders {
		err = enc.encoder.Decode(payload, &heartbeat)
		if err == nil {
			log.Printf("Successfully decoded heartbeat payload with %s", enc.name)
			break
		} else {
			log.Printf("Decoding heartbeat payload with %s failed: %v", enc.name, err)
		}
	}

	if err != nil {
		log.Printf("Failed to decode heartbeat payload. Raw payload (hex): %x", payload)
		return fmt.Errorf("failed to decode heartbeat payload: %v", err)
	}

	// Ensure user is authenticated before processing heartbeat
	if h.userID == "" {
		return fmt.Errorf("not authenticated")
	}

	log.Printf("Received heartbeat from %s: %d", h.userID, heartbeat.Timestamp)
	return h.sendResponse(EventPayload{
		Op: EventHeartbeatAck,
		D:  map[string]interface{}{
			"timestamp": time.Now().UnixMilli(),
		},
	})
}

func (h *WebSocketHandler) handleMessage(payload []byte) error {
	var message MessagePayload
	if err := h.encoder.Decode(payload, &message); err != nil {
		return fmt.Errorf("failed to decode message payload: %v", err)
	}

	log.Printf("Received message from %s in channel %s: %s", 
		h.userID, message.ChannelID, message.Content)

	return h.sendResponse(EventPayload{
		Op: EventMessage,
		D:  message,
	})
}

func (h *WebSocketHandler) sendResponse(response interface{}) error {
	// If the response is not already an EventPayload, wrap it
	var payload EventPayload
	switch v := response.(type) {
	case EventPayload:
		payload = v
	default:
		payload = EventPayload{
			Op: EventDispatch,
			D:  v,
		}
	}

	data, err := h.encoder.Encode(payload)
	if err != nil {
		return fmt.Errorf("failed to encode response: %v", err)
	}

	messageType := websocket.BinaryMessage
	if h.format == format.FormatJSON {
		messageType = websocket.TextMessage
	}

	if err := h.conn.WriteMessage(messageType, data); err != nil {
		return fmt.Errorf("failed to write response: %v", err)
	}

	return nil
}

func (h *WebSocketHandler) Close() {
	log.Printf("Closing WebSocket connection for user %s", h.userID)
	
	// Remove connection from ConnectionManager
	if h.userID != "" {
		Manager.RemoveConnection(h.userID, h)

		// Only proceed with offline status if this was the last connection
		if !Manager.HasOtherConnections(h.userID, h) {
			// Get current user details to check their status
			details, err := h.userRepo.GetUserDetails(h.userID)
			if err != nil {
				log.Printf("Failed to get user details during close: %v", err)
				return
			}

			// Only send presence update if they weren't already showing as offline
			if details.Presence.Status != "offline" {
				log.Printf("Last connection closed for user %s, broadcasting offline status", h.userID)
				if err := Manager.BroadcastPresenceUpdate(h.userID, "offline", details.Presence.CustomStatus, h.userRepo); err != nil {
					log.Printf("Failed to broadcast offline presence update: %v", err)
				}
				
				// Update the user's status in the database
				if err := h.userRepo.SetUserOffline(h.userID, h.sessionToken); err != nil {
					log.Printf("Failed to set user offline: %v", err)
				}
			} else {
				log.Printf("User %s was already showing as offline, skipping presence update", h.userID)
			}
		} else {
			log.Printf("User %s has other active connections, not updating presence", h.userID)
		}
	}

	// Close the underlying WebSocket connection
	if h.conn != nil {
		h.conn.Close()
	}
}
