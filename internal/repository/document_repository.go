package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"doc-manager/internal/domain"
)

var (
	ErrDocumentNotFound = errors.New("document not found")
)

type DocumentRepository struct {
	db *sql.DB
}

func NewDocumentRepository(db *sql.DB) *DocumentRepository {
	return &DocumentRepository{db: db}
}

func (r *DocumentRepository) Create(doc *domain.Document) error {
	query := `INSERT INTO documents (
		id, owner_id, filename, mime_type, size_bytes, storage_path, 
		is_public, deleted_at, purge_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	now := time.Now().UTC()
	doc.CreatedAt = now
	doc.UpdatedAt = now

	isPublicInt := 0
	if doc.IsPublic {
		isPublicInt = 1
	}

	var deletedAtStr, purgeAtStr *string
	if doc.DeletedAt != nil {
		s := doc.DeletedAt.Format(time.RFC3339)
		deletedAtStr = &s
	}
	if doc.PurgeAt != nil {
		s := doc.PurgeAt.Format(time.RFC3339)
		purgeAtStr = &s
	}

	_, err := r.db.Exec(query,
		doc.ID,
		doc.OwnerID,
		doc.Filename,
		doc.MimeType,
		doc.SizeBytes,
		doc.StoragePath,
		isPublicInt,
		deletedAtStr,
		purgeAtStr,
		doc.CreatedAt.Format(time.RFC3339),
		doc.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to insert document: %w", err)
	}

	return nil
}

func (r *DocumentRepository) FindByID(id string) (*domain.Document, error) {
	query := `SELECT id, owner_id, filename, mime_type, size_bytes, storage_path, 
	                 is_public, deleted_at, purge_at, created_at, updated_at
	          FROM documents WHERE id = ?`

	row := r.db.QueryRow(query, id)
	return r.scanDocument(row)
}

func (r *DocumentRepository) ListActiveByOwner(ownerID string, offset, limit int) ([]domain.Document, int, error) {
	countQuery := `SELECT COUNT(*) FROM documents WHERE owner_id = ? AND deleted_at IS NULL`
	var total int
	if err := r.db.QueryRow(countQuery, ownerID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count active documents: %w", err)
	}

	query := `SELECT id, owner_id, filename, mime_type, size_bytes, storage_path, 
	                 is_public, deleted_at, purge_at, created_at, updated_at
	          FROM documents 
	          WHERE owner_id = ? AND deleted_at IS NULL
	          ORDER BY created_at DESC
	          LIMIT ? OFFSET ?`

	rows, err := r.db.Query(query, ownerID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list active documents: %w", err)
	}
	defer rows.Close()

	var docs []domain.Document
	for rows.Next() {
		d, err := r.scanDocumentRow(rows)
		if err != nil {
			return nil, 0, err
		}
		docs = append(docs, *d)
	}

	return docs, total, nil
}

func (r *DocumentRepository) UpdateVisibility(id, ownerID string, isPublic bool) error {
	val := 0
	if isPublic {
		val = 1
	}
	query := `UPDATE documents SET is_public = ?, updated_at = ? WHERE id = ? AND owner_id = ? AND deleted_at IS NULL`
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := r.db.Exec(query, val, now, id, ownerID)
	if err != nil {
		return fmt.Errorf("failed to update document visibility: %w", err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrDocumentNotFound
	}
	return nil
}

func (r *DocumentRepository) SoftDelete(id, ownerID string, retentionDays int) error {
	now := time.Now().UTC()
	purgeAt := now.Add(time.Duration(retentionDays) * 24 * time.Hour)

	query := `UPDATE documents 
	          SET deleted_at = ?, purge_at = ?, updated_at = ?
	          WHERE id = ? AND owner_id = ? AND deleted_at IS NULL`

	res, err := r.db.Exec(query,
		now.Format(time.RFC3339),
		purgeAt.Format(time.RFC3339),
		now.Format(time.RFC3339),
		id,
		ownerID,
	)
	if err != nil {
		return fmt.Errorf("failed to soft delete document: %w", err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrDocumentNotFound
	}
	return nil
}

func (r *DocumentRepository) ListTrashByOwner(ownerID string) ([]domain.TrashDocument, error) {
	query := `SELECT id, owner_id, filename, mime_type, size_bytes, storage_path, 
	                 is_public, deleted_at, purge_at, created_at, updated_at
	          FROM documents 
	          WHERE owner_id = ? AND deleted_at IS NOT NULL
	          ORDER BY deleted_at DESC`

	rows, err := r.db.Query(query, ownerID)
	if err != nil {
		return nil, fmt.Errorf("failed to list trash documents: %w", err)
	}
	defer rows.Close()

	var trashDocs []domain.TrashDocument
	now := time.Now().UTC()

	for rows.Next() {
		d, err := r.scanDocumentRow(rows)
		if err != nil {
			return nil, err
		}

		td := domain.TrashDocument{Document: *d}
		if d.PurgeAt != nil && d.PurgeAt.After(now) {
			diff := d.PurgeAt.Sub(now)
			td.RemainingHours = int64(diff.Hours())
			td.RemainingMinutes = int64(diff.Minutes())
		}
		trashDocs = append(trashDocs, td)
	}

	return trashDocs, nil
}

func (r *DocumentRepository) Restore(id, ownerID string) error {
	now := time.Now().UTC()
	query := `UPDATE documents 
	          SET deleted_at = NULL, purge_at = NULL, updated_at = ?
	          WHERE id = ? AND owner_id = ? AND deleted_at IS NOT NULL`

	res, err := r.db.Exec(query, now.Format(time.RFC3339), id, ownerID)
	if err != nil {
		return fmt.Errorf("failed to restore document: %w", err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrDocumentNotFound
	}
	return nil
}

func (r *DocumentRepository) HardDelete(id string) error {
	query := `DELETE FROM documents WHERE id = ?`
	res, err := r.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to hard delete document: %w", err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return ErrDocumentNotFound
	}
	return nil
}

func (r *DocumentRepository) GetExpiredPurgableDocuments(now time.Time) ([]domain.Document, error) {
	query := `SELECT id, owner_id, filename, mime_type, size_bytes, storage_path, 
	                 is_public, deleted_at, purge_at, created_at, updated_at
	          FROM documents 
	          WHERE deleted_at IS NOT NULL AND purge_at <= ?`

	rows, err := r.db.Query(query, now.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("failed to query purgable documents: %w", err)
	}
	defer rows.Close()

	var docs []domain.Document
	for rows.Next() {
		d, err := r.scanDocumentRow(rows)
		if err != nil {
			return nil, err
		}
		docs = append(docs, *d)
	}
	return docs, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func (r *DocumentRepository) scanDocument(row scannable) (*domain.Document, error) {
	var d domain.Document
	var isPublicInt int
	var deletedAtStr, purgeAtStr sql.NullString
	var createdAtStr, updatedAtStr string

	err := row.Scan(
		&d.ID,
		&d.OwnerID,
		&d.Filename,
		&d.MimeType,
		&d.SizeBytes,
		&d.StoragePath,
		&isPublicInt,
		&deletedAtStr,
		&purgeAtStr,
		&createdAtStr,
		&updatedAtStr,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrDocumentNotFound
		}
		return nil, fmt.Errorf("failed to scan document: %w", err)
	}

	d.IsPublic = isPublicInt == 1
	d.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	d.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAtStr)

	if deletedAtStr.Valid {
		t, _ := time.Parse(time.RFC3339, deletedAtStr.String)
		d.DeletedAt = &t
	}
	if purgeAtStr.Valid {
		t, _ := time.Parse(time.RFC3339, purgeAtStr.String)
		d.PurgeAt = &t
	}

	return &d, nil
}

func (r *DocumentRepository) scanDocumentRow(rows *sql.Rows) (*domain.Document, error) {
	return r.scanDocument(rows)
}
