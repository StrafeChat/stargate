package events

import (
	"fmt"
	"log"
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
}

func NewWebSocketHandler(conn *websocket.Conn, encoder format.Encoder, notificationSvc *services.NotificationService) *WebSocketHandler {
	return &WebSocketHandler{
		conn:             conn,
		encoder:         encoder,
		notificationSvc: notificationSvc,
		userRepo:        repository.NewUserRepository(database.GetSession()),
	}
}

func (h *WebSocketHandler) HandlePayload(messageType int, payload []byte) error {
	
	var base BasePayload
	if err := h.encoder.Decode(payload, &base); err != nil {
		log.Printf("Error decoding base payload: %v", err)
		return err
	}

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
	case PayloadTypePing:
		return h.handlePing()
	default:
		return fmt.Errorf("unknown payload type: %s", base.Type)
	}
}

func (h *WebSocketHandler) handleIdentify(payload []byte) error {
	var identify IdentifyPayload
	if err := h.encoder.Decode(payload, &identify); err != nil {
		return fmt.Errorf("failed to decode identify payload: %v", err)
	}

	userID, err := h.userRepo.ValidateSessionToken(identify.Token)
	if err != nil {
		return fmt.Errorf("invalid token: %v", err)
	}

	h.userID = userID
	h.sessionToken = identify.Token

	if err := h.userRepo.SetUserOnline(userID); err != nil {
		log.Printf("Failed to set user online: %v", err)
	}

	Manager.AddConnection(userID, h)

	details, err := h.userRepo.GetUserDetails(userID)
	if err != nil {
		log.Printf("Failed to get user details: %v", err)
		details = repository.UserDetails{ID: userID}
	}

	ready := ReadyPayload{
		BasePayload: BasePayload{Type: PayloadTypeReady},
		UserID:      details.ID,
		Username:    details.Username,
	}

	return h.sendResponse(ready)
}

func (h *WebSocketHandler) handleHeartbeat(payload []byte) error {
	var heartbeat HeartbeatPayload
	if err := h.encoder.Decode(payload, &heartbeat); err != nil {
		return fmt.Errorf("failed to decode heartbeat payload: %v", err)
	}

	return h.sendResponse(HeartbeatAckPayload{
		BasePayload: BasePayload{Type: PayloadTypeHeartbeatAck},
		Timestamp:   time.Now().UnixMilli(),
	})
}

func (h *WebSocketHandler) handleMessage(payload []byte) error {
	var message MessagePayload
	if err := h.encoder.Decode(payload, &message); err != nil {
		return fmt.Errorf("failed to decode message payload: %v", err)
	}

	log.Printf("Received message from %s in channel %s: %s", 
		h.userID, message.ChannelID, message.Content)

	return nil
}

func (h *WebSocketHandler) handlePing() error {
	return h.sendResponse(PongPayload{
		BasePayload: BasePayload{Type: PayloadTypePong},
	})
}

func (h *WebSocketHandler) sendResponse(response interface{}) error {
	data, err := h.encoder.Encode(response)
	if err != nil {
		return fmt.Errorf("failed to encode response: %v", err)
	}

	if err := h.conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return fmt.Errorf("failed to write response: %v", err)
	}

	return nil
}

func (h *WebSocketHandler) Close() {
	if h.userID != "" && h.sessionToken != "" {
		if !Manager.HasOtherConnections(h.userID, h) {
			if err := h.userRepo.SetUserOffline(h.userID, h.sessionToken); err != nil {
				log.Printf("Failed to set user offline: %v", err)
			}
		}
		Manager.RemoveConnection(h.userID, h)
	}

	if h.conn != nil {
		h.conn.Close()
	}
}
