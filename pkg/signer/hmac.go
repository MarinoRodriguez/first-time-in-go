package signer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"
)

var (
	ErrExpiredSignature = errors.New("download link has expired")
	ErrInvalidSignature = errors.New("invalid signature")
)

type URLSigner struct {
	secretKey []byte
}

func NewURLSigner(secret string) *URLSigner {
	return &URLSigner{
		secretKey: []byte(secret),
	}
}

// GenerateSignature produces an HMAC-SHA256 hex string for a document ID and expiration timestamp.
func (s *URLSigner) GenerateSignature(docID string, expiresAt time.Time) string {
	expiresUnix := strconv.FormatInt(expiresAt.Unix(), 10)
	message := fmt.Sprintf("%s:%s", docID, expiresUnix)

	mac := hmac.New(sha256.New, s.secretKey)
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

// ValidateSignature verifies if the signature matches and if the link has not expired.
func (s *URLSigner) ValidateSignature(docID string, expiresUnixStr string, signature string) error {
	expiresUnix, err := strconv.ParseInt(expiresUnixStr, 10, 64)
	if err != nil {
		return ErrInvalidSignature
	}

	if time.Now().Unix() > expiresUnix {
		return ErrExpiredSignature
	}

	expectedSignature := s.GenerateSignature(docID, time.Unix(expiresUnix, 0))

	// Constant-time comparison to prevent timing attacks
	if !hmac.Equal([]byte(signature), []byte(expectedSignature)) {
		return ErrInvalidSignature
	}

	return nil
}
