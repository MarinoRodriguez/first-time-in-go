package handler

import (
	"errors"
	"net/http"

	"doc-manager/internal/api/middleware"
	"doc-manager/internal/api/response"
	"doc-manager/internal/domain"
	"doc-manager/internal/repository"
	"doc-manager/internal/service"

	"github.com/go-chi/chi/v5"
)

type TrashHandler struct {
	docService *service.DocumentService
}

func NewTrashHandler(docService *service.DocumentService) *TrashHandler {
	return &TrashHandler{docService: docService}
}

func (h *TrashHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())

	trashDocs, err := h.docService.ListTrash(userID)
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "failed to list trash documents")
		return
	}

	if trashDocs == nil {
		trashDocs = []domain.TrashDocument{}
	}

	response.JSON(w, http.StatusOK, trashDocs)
}

func (h *TrashHandler) Restore(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.docService.Restore(id, userID); err != nil {
		if errors.Is(err, repository.ErrDocumentNotFound) {
			response.Error(w, http.StatusNotFound, "document not found in trash")
			return
		}
		response.Error(w, http.StatusInternalServerError, "failed to restore document")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{
		"message": "document restored successfully",
		"id":      id,
	})
}

func (h *TrashHandler) HardDelete(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.docService.HardDelete(id, userID); err != nil {
		if errors.Is(err, repository.ErrDocumentNotFound) {
			response.Error(w, http.StatusNotFound, "document not found")
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			response.Error(w, http.StatusForbidden, err.Error())
			return
		}
		response.Error(w, http.StatusInternalServerError, "failed to delete document permanently")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{
		"message": "document permanently deleted",
		"id":      id,
	})
}
