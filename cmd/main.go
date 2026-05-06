package main

import (
	"log/slog"
	"os"

	"github.com/oyamo/blackoutd/internal/api"
	"github.com/oyamo/blackoutd/internal/ingestor"

	"github.com/joho/godotenv"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	_ = godotenv.Load()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		slog.Error("GEMINI_API_KEY is required")
		os.Exit(1)
	}

	slog.Info("Starting web app and scheduler", "port", port)
	go ingestor.Start(apiKey)
	api.Serve(port)
}
