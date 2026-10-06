package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"doc-manager/internal/domain"
)

var (
	ErrQRSessionNotFound = errors.New("qr session not found")
)

type QRRepository struct {
	db *sql.DB
}

func NewQRRepository(db *sql.DB) *QRRepository {
	return &QRRepository{db: db}
}

func (r *QRRepository) Create(session *domain.QRSession) error {
	query := `INSERT INTO qr_sessions (ticket, status, user_id, session_payload, expires_at, created_at)
	          VALUES (?, ?, ?, ?, ?, ?)`

	now := time.Now().UTC()
	session.CreatedAt = now

	_, err := r.db.Exec(query,
		session.Ticket,
		string(session.Status),
		session.UserID,
		session.SessionPayload,
		session.ExpiresAt.Format(time.RFC3339),
		session.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("failed to create qr session: %w", err)
	}

	return nil
}

func (r *QRRepository) FindByTicket(ticket string) (*domain.QRSession, error) {
	query := `SELECT ticket, status, user_id, session_payload, expires_at, created_at
	          FROM qr_sessions WHERE ticket = ?`

	row := r.db.QueryRow(query, ticket)

	var s domain.QRSession
	var statusStr string
	var userID, sessionPayload sql.NullString
	var expiresAtStr, createdAtStr string

	if err := row.Scan(&s.Ticket, &statusStr, &userID, &sessionPayload, &expiresAtStr, &createdAtStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrQRSessionNotFound
		}
		return nil, fmt.Errorf("failed to scan qr session: %w", err)
	}

	s.Status = domain.QRStatus(statusStr)
	s.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAtStr)
	s.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)

	if userID.Valid {
		s.UserID = &userID.String
	}
	if sessionPayload.Valid {
		s.SessionPayload = &sessionPayload.String
	}

	// Dynamic status adjustment if expired in DB but still marked pending
	if s.Status == domain.QRStatusPending && time.Now().UTC().After(s.ExpiresAt) {
		s.Status = domain.QRStatusExpired
	}

	return &s, nil
}

func (r *QRRepository) AuthorizeTicket(ticket, userID, payload string) error {
	query := `UPDATE qr_sessions 
	          SET status = ?, user_id = ?, session_payload = ?
	          WHERE ticket = ? AND status = ? AND expires_at > ?`

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := r.db.Exec(query,
		string(domain.QRStatusAuthorized),
		userID,
		payload,
		ticket,
		string(domain.QRStatusPending),
		now,
	)
	if err != nil {
		return fmt.Errorf("failed to authorize qr session: %w", err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return errors.New("qr ticket is invalid, expired, or already authorized")
	}

	return nil
}

func (r *QRRepository) MarkConsumed(ticket string) error {
	query := `UPDATE qr_sessions SET status = ? WHERE ticket = ? AND status = ?`
	res, err := r.db.Exec(query, string(domain.QRStatusConsumed), ticket, string(domain.QRStatusAuthorized))
	if err != nil {
		return fmt.Errorf("failed to mark qr session consumed: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return errors.New("cannot consume qr ticket")
	}
	return nil
}

func (r *QRRepository) PurgeExpired(now time.Time) (int64, error) {
	query := `DELETE FROM qr_sessions WHERE expires_at <= ? OR status = ?`
	res, err := r.db.Exec(query, now.Format(time.RFC3339), string(domain.QRStatusConsumed))
	if err != nil {
		return 0, fmt.Errorf("failed to purge expired qr sessions: %w", err)
	}
	return res.RowsAffected()
}
