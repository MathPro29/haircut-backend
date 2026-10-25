package auth

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"barbershop/internal/httpx"
	"barbershop/internal/token"
)

type Service struct {
	repo   *Repository
	tokens *token.Manager
}

func NewService(repo *Repository, tokens *token.Manager) *Service {
	return &Service{repo: repo, tokens: tokens}
}

type Session struct {
	AccessToken  string
	RefreshToken string
	User         AuthUser
}

func invalidCredentials() *httpx.AppError {
	return httpx.Unauthorized("ชื่อผู้ใช้หรือรหัสผ่านไม่ถูกต้อง")
}

func toAuthUser(u *userRecord) AuthUser {
	return AuthUser{
		ID:       u.ID,
		Username: u.Username,
		FullName: u.FullName,
		Role:     u.Role,
		StaffID:  u.StaffID,
	}
}

func (s *Service) Login(ctx context.Context, req LoginRequest) (*Session, error) {
	user, err := s.repo.GetByUsername(ctx, req.Username)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, invalidCredentials()
	}
	if err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, invalidCredentials()
	}
	if !user.IsActive {
		return nil, httpx.Forbidden("บัญชีนี้ถูกปิดใช้งาน กรุณาติดต่อผู้ดูแล")
	}

	session, err := s.issueSession(user)
	if err != nil {
		return nil, err
	}
	if err := s.repo.TouchLogin(ctx, user.ID); err != nil {
		slog.Warn("update last_login_at failed", "error", err)
	}
	return session, nil
}

func (s *Service) Refresh(ctx context.Context, userID string) (*Session, error) {
	user, err := s.repo.GetByID(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.Unauthorized("เซสชันไม่ถูกต้อง กรุณาเข้าสู่ระบบอีกครั้ง")
	}
	if err != nil {
		return nil, err
	}
	if !user.IsActive {
		return nil, httpx.Unauthorized("บัญชีนี้ถูกปิดใช้งาน")
	}
	return s.issueSession(user)
}

func (s *Service) GetUser(ctx context.Context, userID string) (*AuthUser, error) {
	user, err := s.repo.GetByID(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.Unauthorized()
	}
	if err != nil {
		return nil, err
	}
	if !user.IsActive {
		return nil, httpx.Unauthorized("บัญชีนี้ถูกปิดใช้งาน")
	}
	u := toAuthUser(user)
	return &u, nil
}

func (s *Service) issueSession(user *userRecord) (*Session, error) {
	staffID := ""
	if user.StaffID != nil {
		staffID = *user.StaffID
	}
	access, err := s.tokens.GenerateAccessToken(user.ID, user.Role, staffID)
	if err != nil {
		return nil, err
	}
	refresh, err := s.tokens.GenerateRefreshToken(user.ID)
	if err != nil {
		return nil, err
	}
	return &Session{AccessToken: access, RefreshToken: refresh, User: toAuthUser(user)}, nil
}
