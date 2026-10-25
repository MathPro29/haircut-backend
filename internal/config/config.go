package config

import (
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port            string
	DatabaseURL     string
	JWTSecret       string
	Env             string // "dev" | "prod"
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	CORSOrigins     []string
	OwnerUsername   string
	OwnerPassword   string
	OwnerName       string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		Port:            getenv("PORT", "8080"),
		DatabaseURL:     getenv("DATABASE_URL", "postgres://barbershop:barbershop@localhost:5432/barbershop?sslmode=disable"),
		JWTSecret:       os.Getenv("JWT_SECRET"),
		Env:             getenv("ENV", "dev"),
		AccessTokenTTL:  getDuration("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL: getDuration("REFRESH_TOKEN_TTL", 7*24*time.Hour),
		CORSOrigins:     splitCSV(getenv("CORS_ORIGINS", "http://localhost:5173")),
		OwnerUsername:   getenv("OWNER_USERNAME", "owner"),
		OwnerPassword:   os.Getenv("OWNER_PASSWORD"),
		OwnerName:       getenv("OWNER_NAME", "เจ้าของร้าน"),
	}

	if cfg.JWTSecret == "" {
		if cfg.Env == "prod" {
			return nil, errors.New("JWT_SECRET is required in production")
		}
		cfg.JWTSecret = "dev-insecure-secret"
		slog.Warn("JWT_SECRET not set, using insecure dev secret")
	}
	if cfg.OwnerPassword == "" {
		if cfg.Env == "prod" {
			return nil, errors.New("OWNER_PASSWORD is required in production")
		}
		cfg.OwnerPassword = "owner123456"
		slog.Warn("OWNER_PASSWORD not set, using dev default")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		slog.Warn("invalid duration value, using fallback", "key", key, "value", v)
		return fallback
	}
	return d
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
