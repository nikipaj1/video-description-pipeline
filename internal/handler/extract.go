package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/nikipaj1/video-description-pipeline/internal/config"
	"github.com/nikipaj1/video-description-pipeline/internal/r2"
	"github.com/nikipaj1/video-description-pipeline/internal/streams"
)

type ExtractHandler struct {
	cfg *config.Config
	r2  *r2.Client
}

func NewExtractHandler(cfg *config.Config, r2Client *r2.Client) *ExtractHandler {
	return &ExtractHandler{cfg: cfg, r2: r2Client}
}

type extractRequest struct {
	AdID string `json:"ad_id"`
}

type streamResult struct {
	Stream      string `json:"stream"`
	Status      string `json:"status"` // "success" | "error" | "skipped"
	ResultCount int    `json:"result_count"`
	R2Key       string `json:"r2_key,omitempty"`
	Error       string `json:"error,omitempty"`
}

type extractResponse struct {
	AdID             string         `json:"ad_id"`
	Streams          []streamResult `json:"streams"`
	ProcessingTimeMs float64        `json:"processing_time_ms"`
}

func (h *ExtractHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body extractRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.AdID == "" {
		http.Error(w, "ad_id is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Minute)
	defer cancel()

	t0 := time.Now()

	// Download video bytes from R2 (needed for Deepgram)
	videoBytes, err := h.r2.DownloadVideo(ctx, body.AdID)
	if err != nil {
		http.Error(w, fmt.Sprintf("download video: %v", err), http.StatusInternalServerError)
		return
	}

	// Download keyframe metadata (needed for VLM)
	keyframeMetas, err := h.r2.DownloadKeyframeMetadata(ctx, body.AdID)
	if err != nil {
		log.Printf("WARN: no keyframe metadata for %s: %v (VLM will be skipped)", body.AdID, err)
		keyframeMetas = nil
	}

	// Download keyframe images for VLM
	var keyframeInputs []streams.KeyframeInput
	if keyframeMetas != nil {
		images, err := h.r2.DownloadKeyframeImages(ctx, body.AdID, keyframeMetas)
		if err != nil {
			log.Printf("WARN: failed to download keyframe images for %s: %v", body.AdID, err)
		} else {
			for _, m := range keyframeMetas {
				if imgBytes, ok := images[m.R2Key]; ok {
					keyframeInputs = append(keyframeInputs, streams.KeyframeInput{
						FrameIndex:   m.Index,
						TimestampSec: m.TimestampSec,
						ImageBytes:   imgBytes,
					})
				}
			}
		}
	}

	// Run Deepgram + VLM concurrently
	var (
		mu          sync.Mutex
		results     []streamResult
		wg          sync.WaitGroup
	)

	// ASR stream (Deepgram) — starts immediately, only needs video bytes
	if h.cfg.DeepgramAPIKey != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sr := h.runASR(ctx, body.AdID, videoBytes)
			mu.Lock()
			results = append(results, sr)
			mu.Unlock()
		}()
	} else {
		results = append(results, streamResult{
			Stream: "asr", Status: "skipped", Error: "DEEPGRAM_API_KEY not configured",
		})
	}

	// VLM stream (Gemini) — needs keyframe images
	if h.cfg.GeminiAPIKey != "" && len(keyframeInputs) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sr := h.runVLM(ctx, body.AdID, keyframeInputs)
			mu.Lock()
			results = append(results, sr)
			mu.Unlock()
		}()
	} else {
		reason := "GEMINI_API_KEY not configured"
		if len(keyframeInputs) == 0 {
			reason = "no keyframe images available"
		}
		results = append(results, streamResult{
			Stream: "vlm", Status: "skipped", Error: reason,
		})
	}

	wg.Wait()

	elapsed := time.Since(t0).Milliseconds()

	resp := extractResponse{
		AdID:             body.AdID,
		Streams:          results,
		ProcessingTimeMs: float64(elapsed),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *ExtractHandler) runASR(ctx context.Context, adID string, videoBytes []byte) streamResult {
	asrResult, err := streams.RunASR(ctx, videoBytes, h.cfg.DeepgramAPIKey)
	if err != nil {
		log.Printf("ASR failed for %s: %v", adID, err)
		return streamResult{Stream: "asr", Status: "error", Error: err.Error()}
	}

	r2Key := fmt.Sprintf("ads/%s/extraction/asr_results.json", adID)
	if err := h.r2.UploadJSON(ctx, r2Key, asrResult); err != nil {
		log.Printf("ASR upload failed for %s: %v", adID, err)
		return streamResult{Stream: "asr", Status: "error", Error: err.Error()}
	}

	return streamResult{
		Stream:      "asr",
		Status:      "success",
		ResultCount: len(asrResult.Segments),
		R2Key:       r2Key,
	}
}

func (h *ExtractHandler) runVLM(ctx context.Context, adID string, keyframes []streams.KeyframeInput) streamResult {
	vlmResult, err := streams.RunVLM(ctx, keyframes, h.cfg.GeminiAPIKey)
	if err != nil {
		log.Printf("VLM failed for %s: %v", adID, err)
		return streamResult{Stream: "vlm", Status: "error", Error: err.Error()}
	}

	r2Key := fmt.Sprintf("ads/%s/extraction/vlm_results.json", adID)
	if err := h.r2.UploadJSON(ctx, r2Key, vlmResult); err != nil {
		log.Printf("VLM upload failed for %s: %v", adID, err)
		return streamResult{Stream: "vlm", Status: "error", Error: err.Error()}
	}

	return streamResult{
		Stream:      "vlm",
		Status:      "success",
		ResultCount: len(vlmResult.Frames),
		R2Key:       r2Key,
	}
}
