// Package config reads the server configuration from the environment, after loading a local .env file if present.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Config holds every setting the server needs; see .env.example.
type Config struct {
	Addr        string
	DatabaseURL string
	WebDir      string // built web app (moneta-app/apps/web/dist) served at /; empty in dev (Vite serves it)
	MasterKey   string // base64 32 bytes, wraps every user's data key
	JWTSecret   []byte
	// CookieSecure marks the refresh cookie HTTPS-only; off on the plain-HTTP home LAN.
	CookieSecure bool
}

const minJWTSecretLen = 32

// Load reads .env (real environment variables win), then the environment, with development defaults.
func Load() (Config, error) {
	if err := loadDotEnv(".env"); err != nil {
		return Config{}, err
	}
	cfg := Config{
		Addr:         getenv("MONETA_ADDR", ":8080"),
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		WebDir:       os.Getenv("MONETA_WEB_DIR"),
		MasterKey:    os.Getenv("MONETA_MASTER_KEY"),
		JWTSecret:    []byte(os.Getenv("MONETA_JWT_SECRET")),
		CookieSecure: os.Getenv("MONETA_COOKIE_SECURE") == "true",
	}
	switch {
	case cfg.DatabaseURL == "":
		return Config{}, errors.New("DATABASE_URL is not set (see .env.example)")
	case cfg.MasterKey == "":
		return Config{}, errors.New("MONETA_MASTER_KEY is not set (see .env.example)")
	case len(cfg.JWTSecret) < minJWTSecretLen:
		return Config{}, fmt.Errorf("MONETA_JWT_SECRET must be at least %d characters", minJWTSecretLen)
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// loadDotEnv sets KEY=VALUE lines from path that are not already set. A missing file is fine (production can use
// real environment variables). No quoting or expansion: values are taken as written.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, set := os.LookupEnv(key); !set {
			_ = os.Setenv(key, strings.TrimSpace(value))
		}
	}
	return scanner.Err()
}
