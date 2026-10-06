package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"doc-manager/internal/config"
	"doc-manager/internal/domain"
	"doc-manager/internal/repository"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrTicketExpired      = errors.New("qr ticket has expired")
	ErrTicketInvalid      = errors.New("qr ticket is invalid or already consumed")
)

type AuthService struct {
	userRepo *repository.UserRepository
	qrRepo   *repository.QRRepository
	cfg      *config.Config
}

func NewAuthService(userRepo *repository.UserRepository, qrRepo *repository.QRRepository, cfg *config.Config) *AuthService {
	return &AuthService{
		userRepo: userRepo,
		qrRepo:   qrRepo,
		cfg:      cfg,
	}
}

func (s *AuthService) Register(req domain.RegisterRequest) (*domain.AuthResponse, error) {
	if req.Email == "" || req.Password == "" {
		return nil, errors.New("email and password are required")
	}

	// Check if already exists
	existing, _ := s.userRepo.FindByEmail(req.Email)
	if existing != nil {
		return nil, repository.ErrUserAlreadyExists
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	user := domain.User{
		ID:           uuid.New().String(),
		Email:        req.Email,
		PasswordHash: string(hash),
	}

	if err := s.userRepo.Create(&user); err != nil {
		return nil, err
	}

	token, exp, err := s.generateJWT(user.ID, user.Email)
	if err != nil {
		return nil, err
	}

	return &domain.AuthResponse{
		Token:     token,
		TokenType: "Bearer",
		ExpiresIn: exp,
		User:      user,
	}, nil
}

func (s *AuthService) Login(req domain.LoginRequest) (*domain.AuthResponse, error) {
	user, err := s.userRepo.FindByEmail(req.Email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	token, exp, err := s.generateJWT(user.ID, user.Email)
	if err != nil {
		return nil, err
	}

	return &domain.AuthResponse{
		Token:     token,
		TokenType: "Bearer",
		ExpiresIn: exp,
		User:      *user,
	}, nil
}

// GenerateJWT creates a signed JWT for the specified user.
func (s *AuthService) generateJWT(userID, email string) (string, int64, error) {
	expirationTime := time.Now().Add(24 * time.Hour)
	claims := jwt.MapClaims{
		"sub":   userID,
		"email": email,
		"exp":   expirationTime.Unix(),
		"iat":   time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(s.cfg.JWTSecret))
	if err != nil {
		return "", 0, fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, int64(time.Until(expirationTime).Seconds()), nil
}

// ValidateJWT parses and validates a token string, returning the userID and email.
func (s *AuthService) ValidateJWT(tokenStr string) (string, string, error) {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.cfg.JWTSecret), nil
	})

	if err != nil || !token.Valid {
		return "", "", errors.New("invalid or expired token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", "", errors.New("invalid token claims")
	}

	sub, _ := claims["sub"].(string)
	email, _ := claims["email"].(string)
	if sub == "" {
		return "", "", errors.New("missing sub claim")
	}

	return sub, email, nil
}

// InitQR generates a new random ticket valid for 120 seconds.
func (s *AuthService) InitQR() (*domain.QRInitResponse, error) {
	ticketBytes := make([]byte, 24)
	if _, err := rand.Read(ticketBytes); err != nil {
		return nil, fmt.Errorf("failed to generate ticket bytes: %w", err)
	}
	ticket := hex.EncodeToString(ticketBytes)

	now := time.Now().UTC()
	expiresAt := now.Add(s.cfg.QRTicketLifetime)

	session := &domain.QRSession{
		Ticket:    ticket,
		Status:    domain.QRStatusPending,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}

	if err := s.qrRepo.Create(session); err != nil {
		return nil, err
	}

	return &domain.QRInitResponse{
		Ticket:    ticket,
		ExpiresAt: expiresAt,
		ExpiresIn: int64(s.cfg.QRTicketLifetime.Seconds()),
	}, nil
}

// GetQRStatus checks the ticket status. If AUTHORIZED, extracts auth payload and marks CONSUMED.
func (s *AuthService) GetQRStatus(ticket string) (*domain.QRStatusResponse, error) {
	session, err := s.qrRepo.FindByTicket(ticket)
	if err != nil {
		return nil, err
	}

	resp := &domain.QRStatusResponse{
		Ticket:    session.Ticket,
		Status:    session.Status,
		ExpiresAt: session.ExpiresAt,
	}

	if session.Status == domain.QRStatusAuthorized && session.SessionPayload != nil {
		var authData domain.AuthData
		if err := json.Unmarshal([]byte(*session.SessionPayload), &authData); err == nil {
			resp.Auth = &authData
			// Burn ticket after delivering payload (one-time use)
			_ = s.qrRepo.MarkConsumed(ticket)
		}
	}

	return resp, nil
}

// AuthorizeQR marks a ticket as AUTHORIZED with a newly minted token for the authenticated user.
func (s *AuthService) AuthorizeQR(ticket, userID string) error {
	user, err := s.userRepo.FindByID(userID)
	if err != nil {
		return err
	}

	session, err := s.qrRepo.FindByTicket(ticket)
	if err != nil {
		return err
	}

	if session.Status != domain.QRStatusPending {
		return errors.New("qr ticket is not pending")
	}

	if time.Now().UTC().After(session.ExpiresAt) {
		return ErrTicketExpired
	}

	token, exp, err := s.generateJWT(user.ID, user.Email)
	if err != nil {
		return err
	}

	authData := domain.AuthData{
		Token:     token,
		TokenType: "Bearer",
		ExpiresIn: exp,
	}
	payloadBytes, _ := json.Marshal(authData)
	payloadStr := string(payloadBytes)

	return s.qrRepo.AuthorizeTicket(ticket, user.ID, payloadStr)
}
