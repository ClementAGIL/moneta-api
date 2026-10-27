// Package config reads the server configuration from environment variables.
package config

import "os"

// Config holds every setting the server needs; see .env.example.
type Config struct {
	Addr        string
	DatabaseURL string
}

// Load reads the environment, falling back to development defaults.
func Load() Config {
	return Config{
		Addr:        getenv("MONETA_ADDR", ":8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
