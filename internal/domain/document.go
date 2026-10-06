package domain

import "time"

type Document struct {
	ID          string     `json:"id"`
	OwnerID     string     `json:"owner_id"`
	Filename    string     `json:"filename"`
	MimeType    string     `json:"mime_type"`
	SizeBytes   int64      `json:"size_bytes"`
	StoragePath string     `json:"-"`
	IsPublic    bool       `json:"is_public"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
	PurgeAt     *time.Time `json:"purge_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type TrashDocument struct {
	Document
	RemainingHours   int64 `json:"remaining_hours"`
	RemainingMinutes int64 `json:"remaining_minutes"`
}

type UpdateVisibilityRequest struct {
	IsPublic bool `json:"is_public"`
}

type ShareRequest struct {
	ExpiresInMinutes int `json:"expires_in_minutes"`
}

type ShareResponse struct {
	DownloadURL string    `json:"download_url"`
	DocID       string    `json:"doc_id"`
	ExpiresAt   time.Time `json:"expires_at"`
	Signature   string    `json:"signature"`
}
