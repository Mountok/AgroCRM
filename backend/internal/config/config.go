package config

import (
	"net/url"
	"os"
	"strings"
)

type Config struct {
	DatabaseURL    string
	Port           string
	AllowedOrigins []string
}

func (c Config) IsAllowedOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	for _, allowed := range c.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	return parsed.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "0.0.0.0")
}

func Load() Config {
	return Config{
		DatabaseURL:    env("DATABASE_URL", "postgres://agrocrm:agrocrm@localhost:5432/agrocrm?sslmode=disable"),
		Port:           env("PORT", "8080"),
		AllowedOrigins: origins(env("FRONTEND_ORIGIN", "http://localhost:5173,http://127.0.0.1:5173,http://0.0.0.0:5173")),
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func origins(raw string) []string {
	out := []string{}
	for _, part := range strings.Split(raw, ",") {
		origin := strings.TrimSpace(part)
		if origin != "" && origin != "*" {
			out = append(out, origin)
		}
	}
	return out
}
