package streams

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// callGemini (integration with httptest)
// ---------------------------------------------------------------------------

func TestCallGemini_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}
		if !strings.Contains(r.URL.RawQuery, "key=test-api-key") {
			t.Errorf("query = %q, missing API key", r.URL.RawQuery)
		}

		var req geminiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.Contents) != 1 || len(req.Contents[0].Parts) != 2 {
			t.Fatalf("expected 1 content with 2 parts")
		}
		if req.Contents[0].Parts[0].Text == "" {
			t.Error("expected text prompt in first part")
		}
		if req.Contents[0].Parts[1].InlineData == nil {
			t.Fatal("expected inline_data in second part")
		}
		if req.Contents[0].Parts[1].InlineData.MimeType != "image/jpeg" {
			t.Errorf("mime_type = %q", req.Contents[0].Parts[1].InlineData.MimeType)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"text": "  A person holding a product in a bright setting.  "},
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	old := geminiBaseURL
	geminiBaseURL = server.URL
	defer func() { geminiBaseURL = old }()

	desc, err := callGemini(context.Background(), "test-api-key", []byte("fake-jpeg"), "Describe this frame")
	if err != nil {
		t.Fatalf("callGemini error: %v", err)
	}

	expected := "A person holding a product in a bright setting."
	if desc != expected {
		t.Errorf("desc = %q, want %q", desc, expected)
	}
}

func TestCallGemini_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "API key invalid",
			},
		})
	}))
	defer server.Close()

	old := geminiBaseURL
	geminiBaseURL = server.URL
	defer func() { geminiBaseURL = old }()

	_, err := callGemini(context.Background(), "bad-key", []byte("img"), "prompt")
	if err == nil {
		t.Fatal("expected error for API error response")
	}
	if !strings.Contains(err.Error(), "API key invalid") {
		t.Errorf("error = %q, should contain 'API key invalid'", err.Error())
	}
}

func TestCallGemini_EmptyCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"candidates": []any{},
		})
	}))
	defer server.Close()

	old := geminiBaseURL
	geminiBaseURL = server.URL
	defer func() { geminiBaseURL = old }()

	_, err := callGemini(context.Background(), "key", []byte("img"), "prompt")
	if err == nil {
		t.Fatal("expected error for empty candidates")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("error = %q, should mention empty response", err.Error())
	}
}

func TestCallGemini_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer server.Close()

	old := geminiBaseURL
	geminiBaseURL = server.URL
	defer func() { geminiBaseURL = old }()

	_, err := callGemini(context.Background(), "key", []byte("img"), "prompt")
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error = %q, should contain status code", err.Error())
	}
}

// ---------------------------------------------------------------------------
// RunVLM
// ---------------------------------------------------------------------------

func TestRunVLM_SequentialProcessing(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		var req geminiRequest
		json.NewDecoder(r.Body).Decode(&req)

		// Verify that subsequent calls include previous description in prompt
		prompt := req.Contents[0].Parts[0].Text
		if callCount == 1 {
			if !strings.Contains(prompt, "first frame of the ad") {
				t.Errorf("first call should have default context, got: %s", prompt[:80])
			}
		} else if callCount == 2 {
			if !strings.Contains(prompt, "Frame one description") {
				t.Errorf("second call should include previous description in context, got: %s", prompt[:80])
			}
		}

		desc := "Frame one description"
		if callCount == 2 {
			desc = "Frame two description"
		}

		json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{
					"parts": []map[string]any{{"text": desc}},
				}},
			},
		})
	}))
	defer server.Close()

	old := geminiBaseURL
	geminiBaseURL = server.URL
	defer func() { geminiBaseURL = old }()

	keyframes := []KeyframeInput{
		{FrameIndex: 0, TimestampSec: 0.0, ImageBytes: []byte("img1")},
		{FrameIndex: 5, TimestampSec: 2.5, ImageBytes: []byte("img2")},
	}

	result, err := RunVLM(context.Background(), keyframes, "key")
	if err != nil {
		t.Fatalf("RunVLM error: %v", err)
	}

	if len(result.Frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(result.Frames))
	}
	if result.Frames[0].FrameIndex != 0 {
		t.Errorf("frame 0 index = %d", result.Frames[0].FrameIndex)
	}
	if result.Frames[0].Description != "Frame one description" {
		t.Errorf("frame 0 desc = %q", result.Frames[0].Description)
	}
	if result.Frames[1].FrameIndex != 5 {
		t.Errorf("frame 1 index = %d", result.Frames[1].FrameIndex)
	}
	if result.Frames[1].TimestampSec != 2.5 {
		t.Errorf("frame 1 timestamp = %f", result.Frames[1].TimestampSec)
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls, got %d", callCount)
	}
}

func TestRunVLM_ErrorContinues(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		switch callCount {
		case 1:
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("server error"))
		case 2:
			json.NewEncoder(w).Encode(map[string]any{
				"candidates": []map[string]any{
					{"content": map[string]any{
						"parts": []map[string]any{{"text": "Second frame OK"}},
					}},
				},
			})
		}
	}))
	defer server.Close()

	old := geminiBaseURL
	geminiBaseURL = server.URL
	defer func() { geminiBaseURL = old }()

	keyframes := []KeyframeInput{
		{FrameIndex: 0, TimestampSec: 0.0, ImageBytes: []byte("img1")},
		{FrameIndex: 3, TimestampSec: 1.5, ImageBytes: []byte("img2")},
	}

	result, err := RunVLM(context.Background(), keyframes, "key")
	if err != nil {
		t.Fatalf("RunVLM should not return error: %v", err)
	}

	if len(result.Frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(result.Frames))
	}
	// First frame should have error description
	if !strings.Contains(result.Frames[0].Description, "[Error:") {
		t.Errorf("frame 0 should have error, got: %q", result.Frames[0].Description)
	}
	// Second frame should succeed
	if result.Frames[1].Description != "Second frame OK" {
		t.Errorf("frame 1 desc = %q", result.Frames[1].Description)
	}
}

func TestRunVLM_EmptyKeyframes(t *testing.T) {
	result, err := RunVLM(context.Background(), nil, "key")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(result.Frames) != 0 {
		t.Errorf("expected 0 frames, got %d", len(result.Frames))
	}
}

func TestVLMPromptTemplate(t *testing.T) {
	expected := []string{
		"Previous frame context",
		"Timestamp",
		"Camera movement",
		"Emotional tone",
		"motion blur",
	}
	for _, exp := range expected {
		if !strings.Contains(vlmPromptTemplate, exp) {
			t.Errorf("prompt template missing %q", exp)
		}
	}
}
