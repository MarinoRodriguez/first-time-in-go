package signer

import (
	"strconv"
	"testing"
	"time"
)

func TestURLSigner_ValidateSignature(t *testing.T) {
	secret := "test-secret-key-12345"
	urlSigner := NewURLSigner(secret)
	docID := "550e8400-e29b-41d4-a716-446655440000"

	// 1. Valid Signature
	future := time.Now().Add(10 * time.Minute)
	futureUnixStr := strconv.FormatInt(future.Unix(), 10)
	sig := urlSigner.GenerateSignature(docID, future)

	err := urlSigner.ValidateSignature(docID, futureUnixStr, sig)
	if err != nil {
		t.Fatalf("expected valid signature, got error: %v", err)
	}

	// 2. Bad timestamp format
	err = urlSigner.ValidateSignature(docID, "not-a-number", sig)
	if err != ErrInvalidSignature {
		t.Fatalf("expected ErrInvalidSignature for invalid number, got %v", err)
	}

	// 3. Mismatched timestamp
	wrongUnixStr := strconv.FormatInt(future.Add(5*time.Minute).Unix(), 10)
	err = urlSigner.ValidateSignature(docID, wrongUnixStr, sig)
	if err != ErrInvalidSignature {
		t.Fatalf("expected ErrInvalidSignature for altered timestamp, got %v", err)
	}

	// 4. Mismatched docID
	err = urlSigner.ValidateSignature("different-id", futureUnixStr, sig)
	if err != ErrInvalidSignature {
		t.Fatalf("expected ErrInvalidSignature for altered docID, got %v", err)
	}

	// 5. Expired Signature
	past := time.Now().Add(-10 * time.Minute)
	pastUnixStr := strconv.FormatInt(past.Unix(), 10)
	sigPast := urlSigner.GenerateSignature(docID, past)

	err = urlSigner.ValidateSignature(docID, pastUnixStr, sigPast)
	if err != ErrExpiredSignature {
		t.Fatalf("expected ErrExpiredSignature, got %v", err)
	}
}
