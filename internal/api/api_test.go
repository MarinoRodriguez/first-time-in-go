package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"doc-manager/internal/api"
	"doc-manager/internal/config"
	"doc-manager/internal/database"
	"doc-manager/internal/domain"
	"doc-manager/internal/repository"
	"doc-manager/internal/service"
	"doc-manager/internal/storage"
	"doc-manager/pkg/signer"
)

func setupTestServer(t *testing.T) (http.Handler, *config.Config) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	storageDir := filepath.Join(tmpDir, "blobs")

	cfg := &config.Config{
		ServerPort:       "8080",
		DBPath:           dbPath,
		StorageDir:       storageDir,
		JWTSecret:        "test-secret-key-jwt",
		HMACSecret:       "test-secret-key-hmac",
		MaxUploadBytes:   5 * 1024 * 1024,
		WorkerInterval:   time.Minute,
		RetentionDays:    7,
		QRTicketLifetime: 2 * time.Minute,
	}

	db, err := database.InitDB(cfg.DBPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	localStorage, err := storage.NewLocalStorage(cfg.StorageDir)
	if err != nil {
		t.Fatalf("failed to init storage: %v", err)
	}

	userRepo := repository.NewUserRepository(db)
	docRepo := repository.NewDocumentRepository(db)
	qrRepo := repository.NewQRRepository(db)

	urlSigner := signer.NewURLSigner(cfg.HMACSecret)
	authService := service.NewAuthService(userRepo, qrRepo, cfg)
	docService := service.NewDocumentService(docRepo, localStorage, urlSigner, cfg)

	router := api.SetupRouter(api.RouterParams{
		AuthService: authService,
		DocService:  docService,
	})

	return router, cfg
}

func TestCompleteDMSFlow(t *testing.T) {
	router, _ := setupTestServer(t)

	// 1. Register User A
	regBody, _ := json.Marshal(map[string]string{
		"email":    "alice@example.com",
		"password": "Password123!",
	})
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(regBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on register, got %d: %s", w.Code, w.Body.String())
	}

	var authResp domain.AuthResponse
	_ = json.Unmarshal(w.Body.Bytes(), &authResp)
	tokenA := authResp.Token
	if tokenA == "" {
		t.Fatal("expected token to be returned on register")
	}

	// 2. Login User A
	loginBody, _ := json.Marshal(map[string]string{
		"email":    "alice@example.com",
		"password": "Password123!",
	})
	req = httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on login, got %d", w.Code)
	}

	// 3. QR Code Lifecycle (RF-02)
	// 3a. Init QR ticket
	req = httptest.NewRequest("POST", "/api/v1/auth/qr/init", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created on QR init, got %d", w.Code)
	}
	var qrInit domain.QRInitResponse
	_ = json.Unmarshal(w.Body.Bytes(), &qrInit)
	ticket := qrInit.Ticket

	// 3b. Poll status (should be PENDING)
	req = httptest.NewRequest("GET", "/api/v1/auth/qr/status/"+ticket, nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on QR status, got %d", w.Code)
	}
	var qrStatus domain.QRStatusResponse
	_ = json.Unmarshal(w.Body.Bytes(), &qrStatus)
	if qrStatus.Status != domain.QRStatusPending {
		t.Fatalf("expected status PENDING, got %s", qrStatus.Status)
	}

	// 3c. Authorize ticket with Alice's active session
	authQRBody, _ := json.Marshal(map[string]string{"ticket": ticket})
	req = httptest.NewRequest("POST", "/api/v1/auth/qr/authorize", bytes.NewReader(authQRBody))
	req.Header.Set("Authorization", "Bearer "+tokenA)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on QR authorize, got %d: %s", w.Code, w.Body.String())
	}

	// 3d. Poll status again (should be AUTHORIZED and deliver token)
	req = httptest.NewRequest("GET", "/api/v1/auth/qr/status/"+ticket, nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on QR status poll, got %d", w.Code)
	}
	var qrAuthorizedStatus domain.QRStatusResponse
	_ = json.Unmarshal(w.Body.Bytes(), &qrAuthorizedStatus)
	if qrAuthorizedStatus.Status != domain.QRStatusAuthorized || qrAuthorizedStatus.Auth == nil {
		t.Fatalf("expected AUTHORIZED with token payload, got %+v", qrAuthorizedStatus)
	}
	qrToken := qrAuthorizedStatus.Auth.Token

	// 4. Ingest and Magic Bytes validation (RF-03, RF-04)
	// 4a. Attempt to upload invalid plain text file -> Must fail with HTTP 415
	body := new(bytes.Buffer)
	mw := multipart.NewWriter(body)
	fw, _ := mw.CreateFormFile("file", "fake.pdf")
	_, _ = fw.Write([]byte("this is plain text and not a pdf"))
	_ = mw.Close()

	req = httptest.NewRequest("POST", "/api/v1/documents", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+qrToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected HTTP 415 Unsupported Media Type for invalid pdf, got %d: %s", w.Code, w.Body.String())
	}

	// 4b. Upload valid PDF file -> Should succeed HTTP 201
	validPDFContent := []byte("%PDF-1.4\n%test\ntrailer\n<<>>\n%%EOF")
	body = new(bytes.Buffer)
	mw = multipart.NewWriter(body)
	fw, _ = mw.CreateFormFile("file", "report.pdf")
	_, _ = fw.Write(validPDFContent)
	_ = mw.Close()

	req = httptest.NewRequest("POST", "/api/v1/documents", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+qrToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201 Created for valid pdf upload, got %d: %s", w.Code, w.Body.String())
	}

	var createdDoc domain.Document
	_ = json.Unmarshal(w.Body.Bytes(), &createdDoc)
	if createdDoc.ID == "" || createdDoc.Filename != "report.pdf" {
		t.Fatalf("invalid uploaded document response: %+v", createdDoc)
	}

	// 5. Protected Download (RF-05)
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/v1/documents/%s/download", createdDoc.ID), nil)
	req.Header.Set("Authorization", "Bearer "+qrToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on protected download, got %d", w.Code)
	}
	if !bytes.Equal(w.Body.Bytes(), validPDFContent) {
		t.Fatal("downloaded bytes do not match uploaded bytes")
	}

	// 6. Anonymous & Public Downloads (RF-06)
	// 6a. Attempt anonymous download while is_public = 0 -> Must fail 403
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/v1/public/documents/%s/download", createdDoc.ID), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for private doc download, got %d", w.Code)
	}

	// 6b. Make document public
	visBody, _ := json.Marshal(map[string]bool{"is_public": true})
	req = httptest.NewRequest("PATCH", fmt.Sprintf("/api/v1/documents/%s/visibility", createdDoc.ID), bytes.NewReader(visBody))
	req.Header.Set("Authorization", "Bearer "+qrToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on visibility update, got %d", w.Code)
	}

	// 6c. Now anonymous download must succeed
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/v1/public/documents/%s/download", createdDoc.ID), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on public download, got %d", w.Code)
	}

	// 6d. Test Signed Download URL
	shareBody, _ := json.Marshal(map[string]int{"expires_in_minutes": 30})
	req = httptest.NewRequest("POST", fmt.Sprintf("/api/v1/documents/%s/share", createdDoc.ID), bytes.NewReader(shareBody))
	req.Header.Set("Authorization", "Bearer "+qrToken)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on share link generation, got %d", w.Code)
	}

	var shareResp domain.ShareResponse
	_ = json.Unmarshal(w.Body.Bytes(), &shareResp)
	if shareResp.Signature == "" {
		t.Fatal("expected signature in share response")
	}

	// Download via signed link
	signedDownloadURL := fmt.Sprintf("/api/v1/public/documents/signed-download?id=%s&expires=%d&signature=%s",
		createdDoc.ID, shareResp.ExpiresAt.Unix(), shareResp.Signature)
	req = httptest.NewRequest("GET", signedDownloadURL, nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on signed download, got %d: %s", w.Code, w.Body.String())
	}

	// Tampered signature must fail 403
	badSignedURL := fmt.Sprintf("/api/v1/public/documents/signed-download?id=%s&expires=%d&signature=corrupted_signature",
		createdDoc.ID, shareResp.ExpiresAt.Unix())
	req = httptest.NewRequest("GET", badSignedURL, nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on tampered signature, got %d", w.Code)
	}

	// 7. Trash & Lifecycle (RF-07, RF-08)
	// 7a. Soft Delete
	req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/documents/%s", createdDoc.ID), nil)
	req.Header.Set("Authorization", "Bearer "+qrToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on soft delete, got %d", w.Code)
	}

	// 7b. Ensure document does NOT appear in active documents list
	req = httptest.NewRequest("GET", "/api/v1/documents", nil)
	req.Header.Set("Authorization", "Bearer "+qrToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var listResp struct {
		Data  []domain.Document `json:"data"`
		Total int               `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listResp)
	if listResp.Total != 0 {
		t.Fatalf("expected 0 active documents in list, got %d", listResp.Total)
	}

	// 7c. Ensure document appears in trash
	req = httptest.NewRequest("GET", "/api/v1/trash", nil)
	req.Header.Set("Authorization", "Bearer "+qrToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on trash list, got %d", w.Code)
	}
	var trashList []domain.TrashDocument
	_ = json.Unmarshal(w.Body.Bytes(), &trashList)
	if len(trashList) != 1 || trashList[0].ID != createdDoc.ID {
		t.Fatalf("expected document %s in trash, got %+v", createdDoc.ID, trashList)
	}

	// 7d. Restore document
	req = httptest.NewRequest("POST", fmt.Sprintf("/api/v1/trash/%s/restore", createdDoc.ID), nil)
	req.Header.Set("Authorization", "Bearer "+qrToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on restore, got %d", w.Code)
	}

	// 7e. Verify back in active list
	req = httptest.NewRequest("GET", "/api/v1/documents", nil)
	req.Header.Set("Authorization", "Bearer "+qrToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	_ = json.Unmarshal(w.Body.Bytes(), &listResp)
	if listResp.Total != 1 {
		t.Fatalf("expected 1 active document after restore, got %d", listResp.Total)
	}

	// 7f. Move to trash again and perform permanent Hard Delete
	req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/documents/%s", createdDoc.ID), nil)
	req.Header.Set("Authorization", "Bearer "+qrToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/trash/%s", createdDoc.ID), nil)
	req.Header.Set("Authorization", "Bearer "+qrToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on hard delete, got %d", w.Code)
	}

	// 7g. Verify completely removed from trash
	req = httptest.NewRequest("GET", "/api/v1/trash", nil)
	req.Header.Set("Authorization", "Bearer "+qrToken)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	_ = json.Unmarshal(w.Body.Bytes(), &trashList)
	if len(trashList) != 0 {
		t.Fatalf("expected 0 documents in trash after hard delete, got %d", len(trashList))
	}
}

func TestBackgroundWorkerPurge(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "worker_test.db")
	storageDir := filepath.Join(tmpDir, "blobs")

	cfg := &config.Config{
		ServerPort:       "8080",
		DBPath:           dbPath,
		StorageDir:       storageDir,
		JWTSecret:        "jwt-secret",
		HMACSecret:       "hmac-secret",
		MaxUploadBytes:   5 * 1024 * 1024,
		WorkerInterval:   time.Minute,
		RetentionDays:    7,
		QRTicketLifetime: 2 * time.Minute,
	}

	db, _ := database.InitDB(cfg.DBPath)
	t.Cleanup(func() {
		_ = db.Close()
	})
	localStorage, _ := storage.NewLocalStorage(cfg.StorageDir)
	docRepo := repository.NewDocumentRepository(db)
	qrRepo := repository.NewQRRepository(db)
	userRepo := repository.NewUserRepository(db)

	worker := service.NewWorkerService(docRepo, qrRepo, localStorage, cfg)

	// Create a user
	u := &domain.User{ID: "worker-user", Email: "worker@example.com", PasswordHash: "hash"}
	_ = userRepo.Create(u)

	// Create physical blob
	_, filePath, _, _ := localStorage.SaveStream(bytes.NewReader([]byte("%PDF-1.4 mock content")))

	// Create document with purge_at in the past
	past := time.Now().UTC().Add(-2 * time.Hour)
	doc := &domain.Document{
		ID:          "purged-doc-1",
		OwnerID:     u.ID,
		Filename:    "expired.pdf",
		MimeType:    "application/pdf",
		SizeBytes:   20,
		StoragePath: filePath,
		DeletedAt:   &past,
		PurgeAt:     &past,
	}
	_ = docRepo.Create(doc)

	// Create expired QR session
	qr := &domain.QRSession{
		Ticket:    "expired-ticket",
		Status:    domain.QRStatusPending,
		ExpiresAt: past,
	}
	_ = qrRepo.Create(qr)

	// Verify file and DB record exist before worker runs
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("expected file to exist before purge")
	}

	// Trigger worker cycle
	worker.RunCleanupCycle()

	// 1. Verify physical file is gone
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("expected physical file to be removed by worker, but it still exists at %s", filePath)
	}

	// 2. Verify metadata row is deleted
	_, err := docRepo.FindByID("purged-doc-1")
	if err == nil {
		t.Fatal("expected document metadata to be purged from SQLite, but it was found")
	}

	// 3. Verify expired QR session is purged
	_, err = qrRepo.FindByTicket("expired-ticket")
	if err == nil {
		t.Fatal("expected expired QR session to be purged, but it was found")
	}
}
