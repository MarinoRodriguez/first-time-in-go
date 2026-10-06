package service

import (
	"context"
	"log"
	"time"

	"doc-manager/internal/config"
	"doc-manager/internal/repository"
	"doc-manager/internal/storage"
)

type WorkerService struct {
	docRepo *repository.DocumentRepository
	qrRepo  *repository.QRRepository
	storage *storage.LocalStorage
	cfg     *config.Config
}

func NewWorkerService(
	docRepo *repository.DocumentRepository,
	qrRepo *repository.QRRepository,
	storage *storage.LocalStorage,
	cfg *config.Config,
) *WorkerService {
	return &WorkerService{
		docRepo: docRepo,
		qrRepo:  qrRepo,
		storage: storage,
		cfg:     cfg,
	}
}

// Start begins the background maintenance routine until ctx is cancelled.
func (w *WorkerService) Start(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.WorkerInterval)
	defer ticker.Stop()

	log.Printf("[Worker] Background cleanup worker started. Interval: %v", w.cfg.WorkerInterval)

	// Run an initial purge on startup
	w.RunCleanupCycle()

	for {
		select {
		case <-ctx.Done():
			log.Println("[Worker] Background cleaner stopped gracefully.")
			return
		case <-ticker.C:
			w.RunCleanupCycle()
		}
	}
}

// RunCleanupCycle performs one iteration of document purge and QR ticket cleanup.
func (w *WorkerService) RunCleanupCycle() {
	now := time.Now().UTC()

	// 1. Purge expired documents in trash
	purgableDocs, err := w.docRepo.GetExpiredPurgableDocuments(now)
	if err != nil {
		log.Printf("[Worker] Error querying purgable documents: %v", err)
	} else if len(purgableDocs) > 0 {
		log.Printf("[Worker] Found %d documents ready for automatic purge.", len(purgableDocs))
		for _, doc := range purgableDocs {
			// Phase 1: Remove physical blob from disk
			if err := w.storage.DeleteFile(doc.StoragePath); err != nil {
				log.Printf("[Worker] Warning: failed to delete file blob %s for doc %s: %v", doc.StoragePath, doc.ID, err)
			}

			// Phase 2: Delete metadata row from SQLite
			if err := w.docRepo.HardDelete(doc.ID); err != nil {
				log.Printf("[Worker] Error deleting metadata row for doc %s: %v", doc.ID, err)
			} else {
				log.Printf("[Worker] Purged document %s (%s)", doc.ID, doc.Filename)
			}
		}
	}

	// 2. Purge expired or consumed QR sessions
	purgedQR, err := w.qrRepo.PurgeExpired(now)
	if err != nil {
		log.Printf("[Worker] Error purging expired QR sessions: %v", err)
	} else if purgedQR > 0 {
		log.Printf("[Worker] Purged %d expired/consumed QR sessions.", purgedQR)
	}
}
