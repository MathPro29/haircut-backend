package token

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	TypeAccess  = "access"
	TypeRefresh = "refresh"
)

var ErrInvalidToken = errors.New("invalid token")

type Claims struct {
	Role    string `json:"role"`
	StaffID string `json:"sid,omitempty"`
	Type    string `json:"typ"`
	jwt.RegisteredClaims
}

type Manager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewManager(secret string, accessTTL, refreshTTL time.Duration) *Manager {
	return &Manager{secret: []byte(secret), accessTTL: accessTTL, refreshTTL: refreshTTL}
}

func (m *Manager) RefreshTTL() time.Duration { return m.refreshTTL }

func (m *Manager) sign(userID, role, staffID, tokenType string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		Role:    role,
		StaffID: staffID,
		Type:    tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

func (m *Manager) GenerateAccessToken(userID, role, staffID string) (string, error) {
	return m.sign(userID, role, staffID, TypeAccess, m.accessTTL)
}

func (m *Manager) GenerateRefreshToken(userID string) (string, error) {
	return m.sign(userID, "", "", TypeRefresh, m.refreshTTL)
}

func (m *Manager) Parse(raw, tokenType string) (*Claims, error) {
	var claims Claims
	parsed, err := jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (any, error) {
		return m.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !parsed.Valid {
		return nil, ErrInvalidToken
	}
	if claims.Type != tokenType || claims.Subject == "" {
		return nil, ErrInvalidToken
	}
	return &claims, nil
}
