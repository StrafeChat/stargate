package events

type PayloadType string

const (
	PayloadTypeIdentify   PayloadType = "IDENTIFY"
	PayloadTypeHeartbeat  PayloadType = "HEARTBEAT"
	PayloadTypeHeartbeatAck PayloadType = "HEARTBEAT_ACK"
	PayloadTypeMessage    PayloadType = "MESSAGE"
	PayloadTypeReady      PayloadType = "READY"
	PayloadTypePing       PayloadType = "PING"
	PayloadTypePong       PayloadType = "PONG"
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

type PingPayload struct {
	BasePayload
}

type PongPayload struct {
	BasePayload
}
