package events

type PayloadType string

const (
	PayloadTypeHello      PayloadType = "HELLO"
	PayloadTypeIdentify   PayloadType = "IDENTIFY"
	PayloadTypeHeartbeat  PayloadType = "HEARTBEAT"
	PayloadTypeHeartbeatAck PayloadType = "HEARTBEAT_ACK"
	PayloadTypeMessage    PayloadType = "MESSAGE"
	PayloadTypeReady      PayloadType = "READY"
	PayloadTypePresenceUpdate PayloadType = "PRESENCE_UPDATE"
	PayloadTypeSpaceCreate PayloadType = "SPACE_CREATE"
)

type BasePayload struct {
	Type PayloadType `json:"type" msgpack:"type"`
}

type IdentifyPayload struct {
	BasePayload
	Token    string  `json:"token" msgpack:"token"`
	BotToken *string `json:"bot_token,omitempty" msgpack:"bot_token,omitempty"`
	Device   *string `json:"device,omitempty" msgpack:"device,omitempty"`
}

type HeartbeatPayload struct {
	BasePayload
	Timestamp int64 `json:"timestamp" msgpack:"timestamp"`
}

type MessagePayload struct {
	BasePayload
	RoomID      string   `json:"room_id" msgpack:"room_id"`
	Content     string   `json:"content" msgpack:"content"`
	Attachments []string `json:"attachments,omitempty" msgpack:"attachments,omitempty"`
}

type HeartbeatAckPayload struct {
	BasePayload
	Timestamp int64 `json:"timestamp" msgpack:"timestamp"`
}

type PresenceUpdatePayload struct {
	BasePayload
	UserID       string `json:"user_id" msgpack:"user_id"`
	Status       string `json:"status" msgpack:"status"`
	CustomStatus string `json:"custom_status" msgpack:"custom_status"`
}

type PingPayload struct {
	BasePayload
}

type PongPayload struct {
	BasePayload
}

type SpaceCreatePayload struct {
	BasePayload
	Space interface{} `json:"space" msgpack:"space"`
}

type HelloPayload struct {
	BasePayload
	HeartbeatInterval int `json:"heartbeat_interval" msgpack:"heartbeat_interval"`
}
