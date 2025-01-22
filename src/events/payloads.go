package events

type PayloadType string

const (
	PayloadTypeIdentify   PayloadType = "IDENTIFY"
	PayloadTypeHeartbeat  PayloadType = "HEARTBEAT"
	PayloadTypeHeartbeatAck PayloadType = "HEARTBEAT_ACK"
	PayloadTypeMessage    PayloadType = "MESSAGE"
	PayloadTypeReady      PayloadType = "READY"
	PayloadTypePresenceUpdate PayloadType = "PRESENCE_UPDATE"
)

type BasePayload struct {
	Type PayloadType `json:"type" msgpack:"type"`
}

type IdentifyPayload struct {
	BasePayload
	Token  string  `json:"token" msgpack:"token"`
	Device *string `json:"device,omitempty" msgpack:"device,omitempty"`
}

type HeartbeatPayload struct {
	BasePayload
	Timestamp int64 `json:"timestamp" msgpack:"timestamp"`
}

type MessagePayload struct {
	BasePayload
	ChannelID string `json:"channel_id" msgpack:"channel_id"`
	Content   string `json:"content" msgpack:"content"`
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
