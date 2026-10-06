package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"doc-manager/internal/api/middleware"
	"doc-manager/internal/api/response"
	"doc-manager/internal/domain"
	"doc-manager/internal/repository"
	"doc-manager/internal/service"
	"doc-manager/pkg/validator"

	"github.com/go-chi/chi/v5"
)

type DocumentHandler struct {
	docService *service.DocumentService
}

func NewDocumentHandler(docService *service.DocumentService) *DocumentHandler {
	return &DocumentHandler{docService: docService}
}

func (h *DocumentHandler) Upload(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse multipart reader for streaming without loading entire form in memory
	mr, err := r.MultipartReader()
	if err != nil {
		response.Error(w, http.StatusBadRequest, "request must be multipart/form-data")
		return
	}

	var uploadedDoc *domain.Document
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			response.Error(w, http.StatusBadRequest, fmt.Sprintf("error reading multipart: %v", err))
			return
		}

		if part.FormName() == "file" {
			filename := part.FileName()
			if filename == "" {
				filename = "document"
			}

			doc, err := h.docService.Upload(userID, filename, part)
			part.Close()

			if err != nil {
				if errors.Is(err, validator.ErrUnsupportedType) || errors.Is(err, validator.ErrFileTooShort) {
					response.Error(w, http.StatusUnsupportedMediaType, err.Error())
					return
				}
				if errors.Is(err, service.ErrPayloadTooLarge) {
					response.Error(w, http.StatusRequestEntityTooLarge, err.Error())
					return
				}
				response.Error(w, http.StatusInternalServerError, fmt.Sprintf("upload failed: %v", err))
				return
			}

			uploadedDoc = doc
			break
		}
		part.Close()
	}

	if uploadedDoc == nil {
		response.Error(w, http.StatusBadRequest, "form field 'file' is required")
		return
	}

	response.JSON(w, http.StatusCreated, uploadedDoc)
}

func (h *DocumentHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	docs, total, err := h.docService.ListUserDocuments(userID, page, limit)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "failed to list documents")
		return
	}

	if docs == nil {
		docs = []domain.Document{}
	}

	response.JSON(w, http.StatusOK, map[string]any{
		"data":  docs,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}

func (h *DocumentHandler) GetMetadata(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	id := chi.URLParam(r, "id")

	doc, err := h.docService.GetMetadata(id, userID)
	if err != nil {
		if errors.Is(err, repository.ErrDocumentNotFound) {
			response.Error(w, http.StatusNotFound, "document not found")
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			response.Error(w, http.StatusForbidden, err.Error())
			return
		}
		if errors.Is(err, service.ErrDocumentInTrash) {
			response.Error(w, http.StatusGone, err.Error())
			return
		}
		response.Error(w, http.StatusInternalServerError, "failed to retrieve metadata")
		return
	}

	response.JSON(w, http.StatusOK, doc)
}

func (h *DocumentHandler) UpdateVisibility(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	id := chi.URLParam(r, "id")

	var req domain.UpdateVisibilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.docService.UpdateVisibility(id, userID, req.IsPublic); err != nil {
		if errors.Is(err, repository.ErrDocumentNotFound) {
			response.Error(w, http.StatusNotFound, "document not found or already deleted")
			return
		}
		response.Error(w, http.StatusInternalServerError, "failed to update visibility")
		return
	}

	response.JSON(w, http.StatusOK, map[string]any{
		"message":   "visibility updated successfully",
		"id":        id,
		"is_public": req.IsPublic,
	})
}

func (h *DocumentHandler) DownloadProtected(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	id := chi.URLParam(r, "id")

	file, doc, err := h.docService.GetProtectedDownload(id, userID)
	if err != nil {
		if errors.Is(err, repository.ErrDocumentNotFound) {
			response.Error(w, http.StatusNotFound, "document not found")
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			response.Error(w, http.StatusForbidden, err.Error())
			return
		}
		if errors.Is(err, service.ErrDocumentInTrash) {
			response.Error(w, http.StatusGone, err.Error())
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
		// Connection might be closed by client
		return
	}
}

func (h *DocumentHandler) Share(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	id := chi.URLParam(r, "id")

	var req domain.ShareRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, r.Host)

	shareResp, err := h.docService.GenerateSignedURL(id, userID, req.ExpiresInMinutes, baseURL)
	if err != nil {
		if errors.Is(err, repository.ErrDocumentNotFound) {
			response.Error(w, http.StatusNotFound, "document not found")
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			response.Error(w, http.StatusForbidden, err.Error())
			return
		}
		if errors.Is(err, service.ErrDocumentInTrash) {
			response.Error(w, http.StatusGone, err.Error())
			return
		}
		response.Error(w, http.StatusInternalServerError, "failed to generate signed URL")
		return
	}

	response.JSON(w, http.StatusOK, shareResp)
}

func (h *DocumentHandler) SoftDelete(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.docService.SoftDelete(id, userID); err != nil {
		if errors.Is(err, repository.ErrDocumentNotFound) {
			response.Error(w, http.StatusNotFound, "document not found or already deleted")
			return
		}
		response.Error(w, http.StatusInternalServerError, "failed to move document to trash")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{
		"message": "document moved to trash (retention period: 7 days)",
		"id":      id,
	})
}
