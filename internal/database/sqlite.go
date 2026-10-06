package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// InitDB initializes SQLite connection with WAL mode, busy timeout and foreign keys.
func InitDB(dbPath string) (*sql.DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create db directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// PRAGMAs configuration
	pragmas := []string{
		"PRAGMA journal_mode = WAL;",
		"PRAGMA busy_timeout = 5000;",
		"PRAGMA foreign_keys = ON;",
		"PRAGMA synchronous = NORMAL;",
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to execute pragma '%s': %w", pragma, err)
		}
	}

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run database migrations: %w", err)
	}

	log.Println("SQLite database initialized successfully with WAL mode.")
	return db, nil
}

func runMigrations(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS documents (
		id TEXT PRIMARY KEY,
		owner_id TEXT NOT NULL,
		filename TEXT NOT NULL,
		mime_type TEXT NOT NULL,
		size_bytes INTEGER NOT NULL,
		storage_path TEXT NOT NULL,
		is_public INTEGER NOT NULL DEFAULT 0,
		deleted_at TEXT,
		purge_at TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_documents_owner ON documents(owner_id, deleted_at);
	CREATE INDEX IF NOT EXISTS idx_documents_purge ON documents(deleted_at, purge_at);

	CREATE TABLE IF NOT EXISTS qr_sessions (
		ticket TEXT PRIMARY KEY,
		status TEXT NOT NULL,
		user_id TEXT,
		session_payload TEXT,
		expires_at TEXT NOT NULL,
		created_at TEXT NOT NULL,
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_qr_sessions_status ON qr_sessions(status, expires_at);
	`

	_, err := db.Exec(schema)
	return err
}
