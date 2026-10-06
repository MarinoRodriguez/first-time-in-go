package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"doc-manager/internal/api/middleware"
	"doc-manager/internal/api/response"
	"doc-manager/internal/domain"
	"doc-manager/internal/repository"
	"doc-manager/internal/service"

	"github.com/go-chi/chi/v5"
)

type AuthHandler struct {
	authService *service.AuthService
}

func NewAuthHandler(authService *service.AuthService) *AuthHandler {
	return &AuthHandler{authService: authService}
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req domain.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	authResp, err := h.authService.Register(req)
	if err != nil {
		if errors.Is(err, repository.ErrUserAlreadyExists) {
			response.Error(w, http.StatusConflict, err.Error())
			return
		}
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	response.JSON(w, http.StatusCreated, authResp)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req domain.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, http.StatusBadRequest, "invalid request payload")
		return
	}

	authResp, err := h.authService.Login(req)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			response.Error(w, http.StatusUnauthorized, err.Error())
			return
		}
		response.Error(w, http.StatusInternalServerError, "failed to authenticate user")
		return
	}

	response.JSON(w, http.StatusOK, authResp)
}

func (h *AuthHandler) InitQR(w http.ResponseWriter, r *http.Request) {
	initResp, err := h.authService.InitQR()
	if err != nil {
		response.Error(w, http.StatusInternalServerError, "failed to initialize qr session")
		return
	}

	response.JSON(w, http.StatusCreated, initResp)
}

func (h *AuthHandler) GetQRStatus(w http.ResponseWriter, r *http.Request) {
	ticket := chi.URLParam(r, "ticket")
	if ticket == "" {
		response.Error(w, http.StatusBadRequest, "ticket parameter is required")
		return
	}

	statusResp, err := h.authService.GetQRStatus(ticket)
	if err != nil {
		if errors.Is(err, repository.ErrQRSessionNotFound) {
			response.Error(w, http.StatusNotFound, "qr ticket not found")
			return
		}
		response.Error(w, http.StatusInternalServerError, "failed to fetch qr status")
		return
	}

	response.JSON(w, http.StatusOK, statusResp)
}

func (h *AuthHandler) AuthorizeQR(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		response.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req domain.QRAuthorizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Ticket == "" {
		response.Error(w, http.StatusBadRequest, "ticket is required in request body")
		return
	}

	if err := h.authService.AuthorizeQR(req.Ticket, userID); err != nil {
		if errors.Is(err, service.ErrTicketExpired) {
			response.Error(w, http.StatusGone, err.Error())
			return
		}
		response.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{
		"message": "qr session authorized successfully",
		"ticket":  req.Ticket,
	})
}
