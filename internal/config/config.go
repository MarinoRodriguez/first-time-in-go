package config

import (
	"bufio"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServerPort       string
	DBPath           string
	StorageDir       string
	JWTSecret        string
	HMACSecret       string
	MaxUploadBytes   int64
	WorkerInterval   time.Duration
	RetentionDays    int
	QRTicketLifetime time.Duration
}

// LoadEnv loads environment variables from a .env file.
// Handles UTF-8 BOM, CRLF, comments and quotes.
func LoadEnv(filePath string) {
	if filePath == "" {
		filePath = os.Getenv("ENV_FILE")
		if filePath == "" {
			filePath = ".env"
		}
	}

	file, err := os.Open(filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[Config] Warning: could not open env file %s: %v", filePath, err)
		}
		return
	}
	defer file.Close()

	loadedCount := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		// Remove UTF-8 BOM if present
		line = strings.TrimPrefix(line, "\xef\xbb\xbf")
		line = strings.TrimSpace(line)

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		// Strip quotes if present
		if len(val) >= 2 {
			if (strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"")) ||
				(strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'")) {
				val = val[1 : len(val)-1]
			}
		}

		_ = os.Setenv(key, val)
		loadedCount++
	}

	log.Printf("[Config] Successfully loaded %d variables from %s", loadedCount, filePath)
}

func LoadConfig(envFiles ...string) *Config {
	for _, f := range envFiles {
		if f != "" {
			LoadEnv(f)
		}
	}
	// Fallback to default .env if no specific file was passed
	if len(envFiles) == 0 {
		LoadEnv("")
	}

	port := getEnv("PORT", "8080")
	dbPath := getEnv("DB_PATH", "./doc_manager.db")
	storageDir := getEnv("STORAGE_DIR", "./storage/documents")
	jwtSecret := getEnv("JWT_SECRET", "super-secret-jwt-key-change-in-production")
	hmacSecret := getEnv("HMAC_SECRET", "super-secret-hmac-key-change-in-production")

	maxUploadMB, _ := strconv.Atoi(getEnv("MAX_UPLOAD_MB", "50"))
	if maxUploadMB <= 0 {
		maxUploadMB = 50
	}

	workerIntervalMinutes, _ := strconv.Atoi(getEnv("WORKER_INTERVAL_MINUTES", "60"))
	if workerIntervalMinutes <= 0 {
		workerIntervalMinutes = 60
	}

	retentionDays, _ := strconv.Atoi(getEnv("RETENTION_DAYS", "7"))
	if retentionDays <= 0 {
		retentionDays = 7
	}

	qrLifetimeSeconds, _ := strconv.Atoi(getEnv("QR_TICKET_LIFETIME_SECONDS", "120"))
	if qrLifetimeSeconds <= 0 {
		qrLifetimeSeconds = 120
	}

	return &Config{
		ServerPort:       port,
		DBPath:           dbPath,
		StorageDir:       storageDir,
		JWTSecret:        jwtSecret,
		HMACSecret:       hmacSecret,
		MaxUploadBytes:   int64(maxUploadMB) * 1024 * 1024,
		WorkerInterval:   time.Duration(workerIntervalMinutes) * time.Minute,
		RetentionDays:    retentionDays,
		QRTicketLifetime: time.Duration(qrLifetimeSeconds) * time.Second,
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
