package config

import "os"

type Config struct {
	Port          string
	DatabaseURL   string
	TelegramToken string
	LLMBaseURL    string
	LLMAPIKey     string
	LLMModel      string
	Timezone      string
}

func Load() Config {
	return Config{
		Port:          env("PORT", "8080"),
		DatabaseURL:   env("DATABASE_URL", "postgres://family:family@localhost:5432/family_expenses?sslmode=disable"),
		TelegramToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		LLMBaseURL:    env("LLM_BASE_URL", "https://api.openai.com/v1"),
		LLMAPIKey:     os.Getenv("LLM_API_KEY"),
		LLMModel:      env("LLM_MODEL", "gpt-4o-mini"),
		Timezone:      env("APP_TIMEZONE", "Europe/Kyiv"),
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
