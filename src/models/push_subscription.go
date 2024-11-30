package models

type PushSubscription struct {
	UserID      string `json:"user_id"`
	Endpoint    string `json:"endpoint"`
	P256DH      string `json:"p256dh"`
	Auth        string `json:"auth"`
	ExpirationTime *int64 `json:"expiration_time,omitempty"`
}
