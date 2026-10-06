package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"doc-manager/internal/domain"
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrUserAlreadyExists = errors.New("user with this email already exists")
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(user *domain.User) error {
	query := `INSERT INTO users (id, email, password_hash, created_at, updated_at) 
	          VALUES (?, ?, ?, ?, ?)`

	now := time.Now().UTC()
	user.CreatedAt = now
	user.UpdatedAt = now

	_, err := r.db.Exec(query,
		user.ID,
		user.Email,
		user.PasswordHash,
		user.CreatedAt.Format(time.RFC3339),
		user.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// Check for unique constraint violation
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

func (r *UserRepository) FindByEmail(email string) (*domain.User, error) {
	query := `SELECT id, email, password_hash, created_at, updated_at FROM users WHERE email = ?`

	row := r.db.QueryRow(query, email)
	var u domain.User
	var createdAtStr, updatedAtStr string

	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &createdAtStr, &updatedAtStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to query user by email: %w", err)
	}

	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	u.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAtStr)
	return &u, nil
}

func (r *UserRepository) FindByID(id string) (*domain.User, error) {
	query := `SELECT id, email, password_hash, created_at, updated_at FROM users WHERE id = ?`

	row := r.db.QueryRow(query, id)
	var u domain.User
	var createdAtStr, updatedAtStr string

	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &createdAtStr, &updatedAtStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to query user by id: %w", err)
	}

	u.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	u.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAtStr)
	return &u, nil
}
