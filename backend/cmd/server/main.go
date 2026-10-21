package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"family-expenses/backend/internal/bot"
	"family-expenses/backend/internal/config"
	"family-expenses/backend/internal/httpapi"
	"family-expenses/backend/internal/llm"
	"family-expenses/backend/internal/repository"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg := config.Load()
	location, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		log.Fatalf("invalid APP_TIMEZONE: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database connection: %v", err)
	}
	defer pool.Close()
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		log.Fatalf("database ping: %v", err)
	}
	schema, err := os.ReadFile("migrations/001_init.sql")
	if err != nil {
		log.Fatalf("read schema: %v", err)
	}
	if _, err := pool.Exec(ctx, string(schema)); err != nil {
		log.Fatalf("apply schema: %v", err)
	}
	repo := repository.New(pool)
	llmClient := llm.New(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel)
	telegram := bot.New(cfg.TelegramToken, repo, llmClient, location)
	go telegram.Run(ctx)
	handler := httpapi.New(repo, location)
	mux := http.NewServeMux()
	handler.Register(mux)
	server := &http.Server{Addr: ":" + cfg.Port, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer shutdownCancel()
		server.Shutdown(shutdownCtx)
	}()
	log.Printf("HTTP API listening on :%s", cfg.Port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http server: %v", err)
	}
}
