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
	conn            *websocket.Conn
	encoder         format.Encoder
	userRepo        *repository.UserRepository
	notificationSvc *services.NotificationService
	userID          string
	sessionToken    string
	botToken        string
	isBot           bool
	format          format.Format
	connectionID    string // Unique ID for this connection instance
}

// WebSocket Event Types
const (
	EventDispatch           = "DISPATCH"
	EventHello              = "HELLO"
	EventHeartbeat          = "HEARTBEAT"
	EventIdentify           = "IDENTIFY"
	EventReady              = "READY"
	EventHeartbeatAck       = "HEARTBEAT_ACK"
	EventMessage            = "MESSAGE"
	EventRelationshipCreate = "RELATIONSHIP_CREATE"
	EventRelationshipAccept = "RELATIONSHIP_ACCEPT"
	EventRelationshipDelete = "RELATIONSHIP_DELETE"
	EventPresenceUpdate     = "PRESENCE_UPDATE"
	EventCustomEmojiCreate  = "CUSTOM_EMOJI_CREATE"
	EventCustomEmojiDelete  = "CUSTOM_EMOJI_DELETE"
	EventCustomEmojiUpdate  = "CUSTOM_EMOJI_UPDATE"
)

// Standardized event payload structure
type EventPayload struct {
	Op string      `json:"op"`
	D  interface{} `json:"d"`
}

func NewWebSocketHandler(conn *websocket.Conn, r *http.Request) *WebSocketHandler {
	// Generate unique connection ID
	connectionID := fmt.Sprintf("conn_%x", time.Now().UnixNano())

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

	handler := &WebSocketHandler{
		conn:            conn,
		encoder:         encoder,
		userRepo:        repository.GetUserRepository(database.GetSession()),
		notificationSvc: services.NewNotificationService(nil),
		format:          selectedFormat,
		connectionID:    connectionID,
	}

	// Send HELLO event immediately upon connection
	if err := handler.sendHello(); err != nil {
		log.Printf("Failed to send HELLO event: %v", err)
	}

	return handler
}

// sendHello sends a HELLO event with heartbeat interval
func (h *WebSocketHandler) sendHello() error {
	helloPayload := HelloPayload{
		BasePayload: BasePayload{
			Type: PayloadTypeHello,
		},
		HeartbeatInterval: 45000, // 45 seconds
	}

	eventPayload := EventPayload{
		Op: EventHello,
		D:  helloPayload,
	}

	data, err := h.encoder.Encode(eventPayload)
	if err != nil {
		return fmt.Errorf("failed to encode HELLO payload: %v", err)
	}

	return h.conn.WriteMessage(websocket.BinaryMessage, data)
}

func (h *WebSocketHandler) HandlePayload(messageType int, payload []byte) error {
	log.Printf("Received payload: messageType=%d, length=%d, hex=%x", messageType, len(payload), payload)

	// Try decoding with multiple formats
	var base BasePayload
	var err error
	var successfulFormat string

	// List of encoders to try
	encoders := []struct {
		name    string
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
	encoders := []struct {
		name    string
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

	// Determine authentication method and validate
	var userID string
	if identifyPayload.BotToken != nil && *identifyPayload.BotToken != "" {
		// Bot authentication
		userID, err = h.userRepo.ValidateBotToken(*identifyPayload.BotToken)
		if err != nil {
			return fmt.Errorf("bot authentication failed: %v", err)
		}
		h.botToken = *identifyPayload.BotToken
		h.isBot = true
		log.Printf("Bot authenticated with user ID: %s", userID)
	} else {
		// User authentication
		userID, err = h.userRepo.ValidateSessionToken(identifyPayload.Token)
		if err != nil {
			return fmt.Errorf("user authentication failed: %v", err)
		}
		h.sessionToken = identifyPayload.Token
		h.isBot = false
		log.Printf("User authenticated with user ID: %s", userID)
	}

	h.userID = userID

	// Add connection to ConnectionManager
	log.Printf("Adding WebSocket connection for user %s", userID)
	Manager.AddConnection(userID, h)

	// Get user details
	details, err := h.userRepo.GetUserDetails(userID)
	if err != nil {
		log.Printf("Failed to get user details: %v", err)
		details = repository.UserDetails{ID: userID}
	}

	// Initialize variables for user-specific data
	var unreadMessages map[string][]string
	var mentionUnreadMessages map[string][]string
	var relationships []string
	var relationshipRequests []repository.Relationship
	var relatedUserIDs []string
	userIDsToFetch := make(map[string]bool)

	// Set user/bot online and handle presence
	if setOnlineErr := h.userRepo.SetUserOnline(userID); setOnlineErr != nil {
		log.Printf("Failed to set user online: %v", setOnlineErr)
	}

	// Broadcast presence update for both users and bots
	log.Printf("Broadcasting presence update for %s %s", map[bool]string{true: "bot", false: "user"}[h.isBot], userID)
	if broadcastErr := Manager.BroadcastPresenceUpdate(userID, details.Presence.Status, details.Presence.CustomStatus, h.userRepo); broadcastErr != nil {
		log.Printf("Failed to broadcast presence update: %v", broadcastErr)
	}

	// Only handle relationships and unread messages for regular users, not bots
	if !h.isBot {
		// Get unread messages for all rooms
		unreadRepo := repository.NewUnreadRepository(h.userRepo.GetSession())
		unreadMessages, err = unreadRepo.GetUnreadMessagesForUser(userID)
		if err != nil {
			log.Printf("Failed to get unread messages: %v", err)
			unreadMessages = make(map[string][]string)
		} else {
			log.Printf("[WebSocket:READY] Got unread messages for user %s: %+v", userID, unreadMessages)
			if len(unreadMessages) == 0 {
				log.Printf("[WebSocket:READY] Warning: No unread messages found for user %s", userID)
			}
		}

		// Get mention unread messages for all rooms
		mentionUnreadMessages, err = unreadRepo.GetMentionUnreadMessagesForUser(userID)
		if err != nil {
			log.Printf("Failed to get mention unread messages: %v", err)
			mentionUnreadMessages = make(map[string][]string)
		} else {
			log.Printf("[WebSocket:READY] Got mention unread messages for user %s: %+v", userID, mentionUnreadMessages)
			if len(mentionUnreadMessages) == 0 {
				log.Printf("[WebSocket:READY] Warning: No mention unread messages found for user %s", userID)
			}
		}

		// Get user relationships and requests
		relationships, err = h.userRepo.GetUserRelationships(userID)
		if err != nil {
			log.Printf("Failed to get user relationships: %v", err)
			relationships = []string{}
		}
		log.Printf("[WebSocket:READY] Got relationships for user %s: %v", userID, relationships)

		relationshipRequests, err = h.userRepo.GetUserRelationshipRequests(userID)
		if err != nil {
			log.Printf("Failed to get user relationship requests: %v", err)
			relationshipRequests = []repository.Relationship{}
		}
		log.Printf("[WebSocket:READY] Got relationship requests for user %s: %+v", userID, relationshipRequests)

		// Get related user IDs
		relatedUserIDs, err = h.userRepo.GetRelatedUserIDs(userID)
		if err != nil {
			log.Printf("Failed to get related user IDs: %v", err)
			relatedUserIDs = []string{}
		}

		// Add friend IDs to the list of users to fetch if not already included
		for _, id := range relatedUserIDs {
			userIDsToFetch[id] = true
		}
		for _, id := range relationships {
			userIDsToFetch[id] = true
		}
	} else {
		// For bots, initialize empty collections
		unreadMessages = make(map[string][]string)
		mentionUnreadMessages = make(map[string][]string)
		relationships = []string{}
		relationshipRequests = []repository.Relationship{}
		relatedUserIDs = []string{}
		log.Printf("[WebSocket:READY] Bot authenticated, skipping relationship and unread message data")
	}

	// Get user rooms
	rooms, err := h.userRepo.GetUserRooms(userID)
	if err != nil {
		log.Printf("Failed to get user rooms: %v", err)
		rooms = []repository.Room{}
	}
	log.Printf("[WebSocket:READY] Got %d rooms for user %s", len(rooms), userID)

	// Get user spaces
	spaces, err := h.userRepo.GetUserSpaces(userID)
	if err != nil {
		log.Printf("Failed to get user spaces: %v", err)
		spaces = []repository.Space{}
	}
	log.Printf("[WebSocket:READY] Got %d spaces for user %s", len(spaces), userID)

	// Add space member IDs to the list of users to fetch with concurrency
	if len(spaces) > 0 {
		log.Printf("[WebSocket:READY] Fetching space members concurrently for %d spaces", len(spaces))

		// Use channels and goroutines for concurrent space member fetching
		type spaceMemberResult struct {
			spaceID string
			members []repository.SpaceMember
			err     error
		}

		resultChan := make(chan spaceMemberResult, len(spaces))

		// Launch goroutines to fetch space members concurrently
		for _, space := range spaces {
			go func(spaceID string) {
				members, err := h.userRepo.GetSpaceMembersWithRoles(spaceID)
				resultChan <- spaceMemberResult{
					spaceID: spaceID,
					members: members,
					err:     err,
				}
			}(space.ID)
		}

		// Collect results and add member IDs to fetch list
		for i := 0; i < len(spaces); i++ {
			result := <-resultChan
			if result.err != nil {
				log.Printf("Failed to get members for space %s: %v", result.spaceID, result.err)
				continue
			}
			for _, member := range result.members {
				userIDsToFetch[member.UserID] = true
			}
			log.Printf("[WebSocket:READY] Added %d members from space %s to user fetch list", len(result.members), result.spaceID)
		}
		close(resultChan)
	}

	// Enhance rooms with permission overrides for space rooms
	enhancedRooms := make([]map[string]interface{}, len(rooms))
	for i, room := range rooms {
		spaceIDStr := "nil"
		if room.SpaceID != nil {
			spaceIDStr = *room.SpaceID
		}
		log.Printf("[WebSocket:READY] Room %d: ID=%s, Type=%d, SpaceID=%s", i, room.ID, room.Type, spaceIDStr)

		// Convert room to map
		roomData := map[string]interface{}{
			"id":           room.ID,
			"name":         room.Name,
			"type":         room.Type,
			"recipients":   room.Recipients,
			"participants": room.Participants,
			"creator":      room.Creator,
			"parent_id":    room.ParentID,
			"position":     room.Position,
			"space_id":     room.SpaceID,
			"created_at":   room.CreatedAt,
			"updated_at":   room.UpdatedAt,
		}

		// Add permission overrides for space rooms (types 2, 3, 4)
		if room.Type == 2 || room.Type == 3 || room.Type == 4 {
			permissionOverrides, err := h.userRepo.GetRoomPermissionOverrides(room.ID, userID)
			if err != nil {
				log.Printf("[WebSocket:READY] Failed to get permission overrides for room %s: %v", room.ID, err)
			} else if permissionOverrides != nil {
				roomData["permission_overrides"] = permissionOverrides
				log.Printf("[WebSocket:READY] Added permission overrides to room %s", room.ID)
			}
		}

		enhancedRooms[i] = roomData
	}

	// Enhance spaces with members and roles data using concurrent fetching
	enhancedSpaces := make([]map[string]interface{}, len(spaces))
	if len(spaces) > 0 {
		log.Printf("[WebSocket:READY] Enhancing %d spaces with members and roles concurrently", len(spaces))

		// Use channels and goroutines for concurrent space enhancement
		type spaceEnhancementResult struct {
			index   int
			space   repository.Space
			members []repository.SpaceMember
			roles   []repository.SpaceRole
			err     error
		}

		enhancementChan := make(chan spaceEnhancementResult, len(spaces))

		// Launch goroutines to enhance spaces concurrently
		for i, space := range spaces {
			go func(index int, sp repository.Space) {
				// Get members with roles for this space
				members, membersErr := h.userRepo.GetSpaceMembersWithRoles(sp.ID)
				if membersErr != nil {
					log.Printf("Failed to get members for space %s: %v", sp.ID, membersErr)
					members = []repository.SpaceMember{}
				}

				// Get roles for this space
				roles, rolesErr := h.userRepo.GetSpaceRoles(sp.ID)
				if rolesErr != nil {
					log.Printf("Failed to get roles for space %s: %v", sp.ID, rolesErr)
					roles = []repository.SpaceRole{}
				}

				enhancementChan <- spaceEnhancementResult{
					index:   index,
					space:   sp,
					members: members,
					roles:   roles,
					err:     nil,
				}
			}(i, space)
		}

		// Collect results and build enhanced spaces
		for i := 0; i < len(spaces); i++ {
			result := <-enhancementChan
			log.Printf("[WebSocket:READY] Enhanced space %s with %d members and %d roles", result.space.ID, len(result.members), len(result.roles))

			// Create enhanced space object with embedded members and roles
			enhancedSpaces[result.index] = map[string]interface{}{
				"id":           result.space.ID,
				"name":         result.space.Name,
				"name_acronym": result.space.NameAcronym,
				"icon":         result.space.Icon,
				"banner":       result.space.Banner,
				"description":  result.space.Description,
				"owner_id":     result.space.OwnerID,
				"created_at":   result.space.CreatedAt,
				"updated_at":   result.space.UpdatedAt,
				"members":      result.members,
				"roles":        result.roles,
			}
		}
		close(enhancementChan)
	}

	// Add recipient IDs from group PMs
	for _, room := range rooms {
		if room.Type == 1 {
			for _, recipientID := range room.Recipients {
				userIDsToFetch[recipientID] = true
			}
		}
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
			"client_user":             details,
			"users":                   relatedUsers,
			"relationships":           relationships,
			"relationship_requests":   relationshipRequests,
			"rooms":                   enhancedRooms,
			"spaces":                  enhancedSpaces,
			"unread_messages":         unreadMessages,
			"mention_unread_messages": mentionUnreadMessages,
		},
	}

	return h.sendResponse(readyPayload)
}

func (h *WebSocketHandler) handleHeartbeat(payload []byte) error {
	var heartbeat HeartbeatPayload
	var err error

	// Try multiple encoders
	encoders := []struct {
		name    string
		encoder format.Encoder
	}{
		{"Primary", h.encoder},
		{"JSON", &format.JSONEncoder{}},
		{"MessagePack", &format.MsgPackEncoder{}},
	}

	for _, enc := range encoders {
		err = enc.encoder.Decode(payload, &heartbeat)
		if err == nil {
			log.Printf("[%s] Successfully decoded heartbeat payload with %s", h.connectionID, enc.name)
			break
		} else {
			log.Printf("[%s] Decoding heartbeat payload with %s failed: %v", h.connectionID, enc.name, err)
		}
	}

	if err != nil {
		log.Printf("[%s] Failed to decode heartbeat payload. Raw payload (hex): %x", h.connectionID, payload)
		return fmt.Errorf("failed to decode heartbeat payload for connection %s: %v", h.connectionID, err)
	}

	// Ensure user is authenticated before processing heartbeat
	if h.userID == "" {
		return fmt.Errorf("connection %s not authenticated", h.connectionID)
	}

	log.Printf("[%s] Received heartbeat from user %s: %d", h.connectionID, h.userID, heartbeat.Timestamp)
	return h.sendResponse(EventPayload{
		Op: EventHeartbeatAck,
		D: map[string]interface{}{
			"timestamp":     time.Now().UnixMilli(),
			"connection_id": h.connectionID,
		},
	})
}

func (h *WebSocketHandler) handleMessage(payload []byte) error {
	var message MessagePayload
	if err := h.encoder.Decode(payload, &message); err != nil {
		return fmt.Errorf("failed to decode message payload: %v", err)
	}

	log.Printf("Received message from %s in room %s: %s",
		h.userID, message.RoomID, message.Content)

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

		// Handle presence updates for both users and bots
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
				log.Printf("Last connection closed for %s %s, broadcasting offline status", map[bool]string{true: "bot", false: "user"}[h.isBot], h.userID)
				if err := Manager.BroadcastPresenceUpdate(h.userID, "offline", details.Presence.CustomStatus, h.userRepo); err != nil {
					log.Printf("Failed to broadcast offline presence update: %v", err)
				}

				// Update the user's status in the database
				if h.isBot {
					// For bots, use bot token instead of session token
					if err := h.userRepo.SetUserOffline(h.userID, h.botToken); err != nil {
						log.Printf("Failed to set bot offline: %v", err)
					}
				} else {
					// For users, use session token
					if err := h.userRepo.SetUserOffline(h.userID, h.sessionToken); err != nil {
						log.Printf("Failed to set user offline: %v", err)
					}
				}
			} else {
				log.Printf("%s %s was already showing as offline, skipping presence update", map[bool]string{true: "Bot", false: "User"}[h.isBot], h.userID)
			}
		} else {
			log.Printf("%s %s has other active connections, not updating presence", map[bool]string{true: "Bot", false: "User"}[h.isBot], h.userID)
		}
	}

	// Close the underlying WebSocket connection
	if h.conn != nil {
		h.conn.Close()
	}
}
