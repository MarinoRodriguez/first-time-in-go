package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"doc-manager/internal/config"
	"doc-manager/internal/domain"
	"doc-manager/internal/repository"
	"doc-manager/internal/storage"
	"doc-manager/pkg/signer"
	"doc-manager/pkg/validator"
)

var (
	ErrForbidden          = errors.New("access denied: you are not the owner of this document")
	ErrPayloadTooLarge    = errors.New("file exceeds maximum allowed upload size")
	ErrDocumentInTrash    = errors.New("document is in trash")
	ErrDocumentNotPublic  = errors.New("document is not public")
)

type DocumentService struct {
	docRepo *repository.DocumentRepository
	storage *storage.LocalStorage
	signer  *signer.URLSigner
	cfg     *config.Config
}

func NewDocumentService(
	docRepo *repository.DocumentRepository,
	storage *storage.LocalStorage,
	signer *signer.URLSigner,
	cfg *config.Config,
) *DocumentService {
	return &DocumentService{
		docRepo: docRepo,
		storage: storage,
		signer:  signer,
		cfg:     cfg,
	}
}

// Upload handles streaming ingest, size verification, magic bytes validation, and atomic storage.
func (s *DocumentService) Upload(ownerID, originalFilename string, r io.Reader) (*domain.Document, error) {
	// Create temporary file to stream upload without holding everything in memory
	tempFile, err := os.CreateTemp("", "dms-upload-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		tempFile.Close()
		_ = os.Remove(tempPath)
	}()

	// Limit reader to cfg.MaxUploadBytes + 1 to detect overflow
	limitedReader := io.LimitReader(r, s.cfg.MaxUploadBytes+1)
	written, err := io.Copy(tempFile, limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to stream upload to disk: %w", err)
	}

	if written > s.cfg.MaxUploadBytes {
		return nil, ErrPayloadTooLarge
	}

	tempFile.Close()

	// Strict Magic Bytes validation
	docType, err := validator.ValidateFile(tempPath)
	if err != nil {
		return nil, err
	}

	// Move/Stream to persistent storage
	tempReader, err := os.Open(tempPath)
	if err != nil {
		return nil, fmt.Errorf("failed to reopen temp file: %w", err)
	}
	defer tempReader.Close()

	docID, storagePath, size, err := s.storage.SaveStream(tempReader)
	if err != nil {
		return nil, fmt.Errorf("failed to store file permanently: %w", err)
	}

	doc := &domain.Document{
		ID:          docID,
		OwnerID:     ownerID,
		Filename:    filepath.Base(originalFilename),
		MimeType:    string(docType),
		SizeBytes:   size,
		StoragePath: storagePath,
		IsPublic:    false,
	}

	if err := s.docRepo.Create(doc); err != nil {
		// Clean up stored file if DB insert fails
		_ = s.storage.DeleteFile(storagePath)
		return nil, fmt.Errorf("failed to save document metadata: %w", err)
	}

	return doc, nil
}

func (s *DocumentService) ListUserDocuments(ownerID string, page, limit int) ([]domain.Document, int, error) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit
	return s.docRepo.ListActiveByOwner(ownerID, offset, limit)
}

func (s *DocumentService) GetMetadata(docID, ownerID string) (*domain.Document, error) {
	doc, err := s.docRepo.FindByID(docID)
	if err != nil {
		return nil, err
	}

	if doc.OwnerID != ownerID {
		return nil, ErrForbidden
	}

	if doc.DeletedAt != nil {
		return nil, ErrDocumentInTrash
	}

	return doc, nil
}

func (s *DocumentService) UpdateVisibility(docID, ownerID string, isPublic bool) error {
	return s.docRepo.UpdateVisibility(docID, ownerID, isPublic)
}

// GetProtectedDownload returns file stream for owner
func (s *DocumentService) GetProtectedDownload(docID, ownerID string) (*os.File, *domain.Document, error) {
	doc, err := s.docRepo.FindByID(docID)
	if err != nil {
		return nil, nil, err
	}

	if doc.OwnerID != ownerID {
		return nil, nil, ErrForbidden
	}

	if doc.DeletedAt != nil {
		return nil, nil, ErrDocumentInTrash
	}

	file, err := s.storage.OpenFile(doc.StoragePath)
	if err != nil {
		return nil, nil, fmt.Errorf("file storage error: %w", err)
	}

	return file, doc, nil
}

// GetPublicDownload returns file stream if marked is_public == 1
func (s *DocumentService) GetPublicDownload(docID string) (*os.File, *domain.Document, error) {
	doc, err := s.docRepo.FindByID(docID)
	if err != nil {
		return nil, nil, err
	}

	if !doc.IsPublic {
		return nil, nil, ErrDocumentNotPublic
	}

	if doc.DeletedAt != nil {
		return nil, nil, ErrDocumentInTrash
	}

	file, err := s.storage.OpenFile(doc.StoragePath)
	if err != nil {
		return nil, nil, fmt.Errorf("file storage error: %w", err)
	}

	return file, doc, nil
}

// GenerateSignedURL generates HMAC-SHA256 signed download link for the owner
func (s *DocumentService) GenerateSignedURL(docID, ownerID string, durationMinutes int, baseURL string) (*domain.ShareResponse, error) {
	doc, err := s.docRepo.FindByID(docID)
	if err != nil {
		return nil, err
	}

	if doc.OwnerID != ownerID {
		return nil, ErrForbidden
	}

	if doc.DeletedAt != nil {
		return nil, ErrDocumentInTrash
	}

	if durationMinutes <= 0 {
		durationMinutes = 60 // default 1 hour
	}

	expiresAt := time.Now().Add(time.Duration(durationMinutes) * time.Minute)
	signature := s.signer.GenerateSignature(doc.ID, expiresAt)

	downloadURL := fmt.Sprintf("%s/api/v1/public/documents/signed-download?id=%s&expires=%d&signature=%s",
		baseURL, doc.ID, expiresAt.Unix(), signature)

	return &domain.ShareResponse{
		DownloadURL: downloadURL,
		DocID:       doc.ID,
		ExpiresAt:   expiresAt,
		Signature:   signature,
	}, nil
}

// GetSignedDownload validates HMAC-SHA256 signature and returns file stream
func (s *DocumentService) GetSignedDownload(docID, expiresStr, signature string) (*os.File, *domain.Document, error) {
	if err := s.signer.ValidateSignature(docID, expiresStr, signature); err != nil {
		return nil, nil, err
	}

	doc, err := s.docRepo.FindByID(docID)
	if err != nil {
		return nil, nil, err
	}

	if doc.DeletedAt != nil {
		return nil, nil, ErrDocumentInTrash
	}

	file, err := s.storage.OpenFile(doc.StoragePath)
	if err != nil {
		return nil, nil, fmt.Errorf("file storage error: %w", err)
	}

	return file, doc, nil
}

// SoftDelete moves document to trash with 7 days retention
func (s *DocumentService) SoftDelete(docID, ownerID string) error {
	return s.docRepo.SoftDelete(docID, ownerID, s.cfg.RetentionDays)
}

func (s *DocumentService) ListTrash(ownerID string) ([]domain.TrashDocument, error) {
	return s.docRepo.ListTrashByOwner(ownerID)
}

func (s *DocumentService) Restore(docID, ownerID string) error {
	return s.docRepo.Restore(docID, ownerID)
}

// HardDelete immediately removes physical file and metadata row
func (s *DocumentService) HardDelete(docID, ownerID string) error {
	doc, err := s.docRepo.FindByID(docID)
	if err != nil {
		return err
	}

	if doc.OwnerID != ownerID {
		return ErrForbidden
	}

	// 1. Delete physical file
	_ = s.storage.DeleteFile(doc.StoragePath)

	// 2. Delete database record
	return s.docRepo.HardDelete(docID)
}
