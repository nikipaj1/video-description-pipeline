package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/nikipaj1/video-description-pipeline/internal/config"
	"github.com/nikipaj1/video-description-pipeline/internal/handler"
	"github.com/nikipaj1/video-description-pipeline/internal/r2"
)

func main() {
	cfg := config.Load()

	r2Client := r2.NewClient(
		cfg.R2EndpointURL,
		cfg.R2AccessKeyID,
		cfg.R2SecretAccessKey,
		cfg.R2Bucket,
	)

	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"streams": map[string]bool{
				"deepgram": cfg.DeepgramAPIKey != "",
				"vlm":      cfg.GeminiAPIKey != "",
			},
		})
	})

	// Extract endpoint
	mux.Handle("POST /extract", handler.NewExtractHandler(cfg, r2Client))

	addr := ":" + cfg.Port
	log.Printf("video-description-pipeline listening on %s", addr)
	log.Printf("  deepgram: configured=%v", cfg.DeepgramAPIKey != "")
	log.Printf("  gemini:   configured=%v", cfg.GeminiAPIKey != "")

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
