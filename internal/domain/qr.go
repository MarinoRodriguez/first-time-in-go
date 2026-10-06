package domain

import "time"

type QRStatus string

const (
	QRStatusPending    QRStatus = "PENDING"
	QRStatusAuthorized QRStatus = "AUTHORIZED"
	QRStatusExpired    QRStatus = "EXPIRED"
	QRStatusConsumed   QRStatus = "CONSUMED"
)

type QRSession struct {
	Ticket         string    `json:"ticket"`
	Status         QRStatus  `json:"status"`
	UserID         *string   `json:"user_id,omitempty"`
	SessionPayload *string   `json:"session_payload,omitempty"`
	ExpiresAt      time.Time `json:"expires_at"`
	CreatedAt      time.Time `json:"created_at"`
}

type QRInitResponse struct {
	Ticket    string    `json:"ticket"`
	ExpiresAt time.Time `json:"expires_at"`
	ExpiresIn int64     `json:"expires_in"` // seconds
}

type QRStatusResponse struct {
	Ticket    string    `json:"ticket"`
	Status    QRStatus  `json:"status"`
	Auth      *AuthData `json:"auth,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

type AuthData struct {
	Token     string `json:"token"`
	TokenType string `json:"token_type"`
	ExpiresIn int64  `json:"expires_in"`
}

type QRAuthorizeRequest struct {
	Ticket string `json:"ticket"`
}
