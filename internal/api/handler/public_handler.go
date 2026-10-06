package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"doc-manager/internal/api/response"
	"doc-manager/internal/repository"
	"doc-manager/internal/service"
	"doc-manager/pkg/signer"

	"github.com/go-chi/chi/v5"
)

type PublicHandler struct {
	docService *service.DocumentService
}

func NewPublicHandler(docService *service.DocumentService) *PublicHandler {
	return &PublicHandler{docService: docService}
}

// DownloadPublic serves files marked as is_public = 1
func (h *PublicHandler) DownloadPublic(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		response.Error(w, http.StatusBadRequest, "document id is required")
		return
	}

	file, doc, err := h.docService.GetPublicDownload(id)
	if err != nil {
		if errors.Is(err, repository.ErrDocumentNotFound) {
			response.Error(w, http.StatusNotFound, "document not found")
			return
		}
		if errors.Is(err, service.ErrDocumentNotPublic) {
			response.Error(w, http.StatusForbidden, "document is not public")
			return
		}
		if errors.Is(err, service.ErrDocumentInTrash) {
			response.Error(w, http.StatusGone, "document is no longer available")
			return
		}
		response.Error(w, http.StatusInternalServerError, "failed to open document")
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", doc.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, doc.Filename))
	w.Header().Set("Content-Length", strconv.FormatInt(doc.SizeBytes, 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if _, err := io.Copy(w, file); err != nil {
		return
	}
}

// DownloadSigned validates HMAC signature and expiration
func (h *PublicHandler) DownloadSigned(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	expires := r.URL.Query().Get("expires")
	sig := r.URL.Query().Get("signature")

	if id == "" || expires == "" || sig == "" {
		response.Error(w, http.StatusBadRequest, "query parameters 'id', 'expires', and 'signature' are required")
		return
	}

	file, doc, err := h.docService.GetSignedDownload(id, expires, sig)
	if err != nil {
		if errors.Is(err, signer.ErrExpiredSignature) {
			response.Error(w, http.StatusGone, "download link has expired")
			return
		}
		if errors.Is(err, signer.ErrInvalidSignature) {
			response.Error(w, http.StatusForbidden, "invalid or tampered signature")
			return
		}
		if errors.Is(err, repository.ErrDocumentNotFound) {
			response.Error(w, http.StatusNotFound, "document not found")
			return
		}
		if errors.Is(err, service.ErrDocumentInTrash) {
			response.Error(w, http.StatusGone, "document is in trash")
			return
		}
		response.Error(w, http.StatusInternalServerError, "failed to retrieve document")
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", doc.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, doc.Filename))
	w.Header().Set("Content-Length", strconv.FormatInt(doc.SizeBytes, 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if _, err := io.Copy(w, file); err != nil {
		return
	}
}
