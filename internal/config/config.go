// Package config reads the server configuration from environment variables.
package config

import "os"

// Config holds every setting the server needs; see .env.example.
type Config struct {
	Addr        string
	DatabaseURL string
	WebDir      string // built web app (moneta-app/apps/web/dist) served at /; empty in dev (Vite serves it)
}

// Load reads the environment, falling back to development defaults.
func Load() Config {
	return Config{
		Addr:        getenv("MONETA_ADDR", ":8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		WebDir:      os.Getenv("MONETA_WEB_DIR"),
	}
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
