package streams

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// groupWordsIntoChunks (pure function)
// ---------------------------------------------------------------------------

func TestGroupWordsIntoChunks_BasicChunking(t *testing.T) {
	// Chunk boundary triggers when w.End - chunkStart >= chunkDuration
	// Words span 0.0 to 3.5s total, chunk duration = 3.0s
	// "a" ends at 3.2, 3.2 - 0.0 = 3.2 >= 3.0 → flush after "a"
	words := []wordEntry{
		{Word: "Hello", Start: 0.0, End: 0.5},
		{Word: "world", Start: 0.6, End: 1.0},
		{Word: "this", Start: 1.1, End: 1.5},
		{Word: "is", Start: 1.6, End: 2.0},
		{Word: "a", Start: 3.0, End: 3.2},
		{Word: "test", Start: 3.3, End: 3.5},
	}

	segments := groupWordsIntoChunks(words, 3.0)

	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	if segments[0].Text != "Hello world this is a" {
		t.Errorf("segment 0 text = %q, want %q", segments[0].Text, "Hello world this is a")
	}
	if segments[0].Start != 0.0 || segments[0].End != 3.2 {
		t.Errorf("segment 0 times = (%.1f, %.1f), want (0.0, 3.2)", segments[0].Start, segments[0].End)
	}
	if segments[1].Text != "test" {
		t.Errorf("segment 1 text = %q, want %q", segments[1].Text, "test")
	}
}

func TestGroupWordsIntoChunks_Empty(t *testing.T) {
	segments := groupWordsIntoChunks(nil, 3.0)
	if len(segments) != 0 {
		t.Fatalf("expected 0 segments, got %d", len(segments))
	}
}

func TestGroupWordsIntoChunks_SingleWord(t *testing.T) {
	words := []wordEntry{
		{Word: "hello", Start: 0.0, End: 0.5},
	}
	segments := groupWordsIntoChunks(words, 3.0)

	if len(segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segments))
	}
	if segments[0].Text != "hello" {
		t.Errorf("text = %q, want %q", segments[0].Text, "hello")
	}
	if segments[0].Start != 0.0 || segments[0].End != 0.5 {
		t.Errorf("times = (%.1f, %.1f), want (0.0, 0.5)", segments[0].Start, segments[0].End)
	}
}

func TestGroupWordsIntoChunks_ExactBoundary(t *testing.T) {
	words := []wordEntry{
		{Word: "a", Start: 0.0, End: 1.0},
		{Word: "b", Start: 1.0, End: 2.0},
		{Word: "c", Start: 2.0, End: 3.0},
		{Word: "d", Start: 3.5, End: 4.0},
	}
	segments := groupWordsIntoChunks(words, 3.0)

	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	if segments[0].Text != "a b c" {
		t.Errorf("segment 0 = %q, want %q", segments[0].Text, "a b c")
	}
	if segments[1].Text != "d" {
		t.Errorf("segment 1 = %q, want %q", segments[1].Text, "d")
	}
}

func TestGroupWordsIntoChunks_LongGap(t *testing.T) {
	words := []wordEntry{
		{Word: "first", Start: 0.0, End: 0.5},
		{Word: "second", Start: 10.0, End: 10.5},
	}
	segments := groupWordsIntoChunks(words, 3.0)

	// "second" starts at 10.0, but chunk started at 0.0 → gap > 3s → "first" flushed
	// Actually: end(0.5) - start(0.0) = 0.5 < 3.0, then end(10.5) - start(0.0) = 10.5 >= 3.0 → flush
	if len(segments) != 1 {
		t.Fatalf("expected 1 segment (all in one chunk until boundary), got %d", len(segments))
	}
	if segments[0].Text != "first second" {
		t.Errorf("text = %q", segments[0].Text)
	}
}

// ---------------------------------------------------------------------------
// RunASR (integration with httptest)
// ---------------------------------------------------------------------------

func TestRunASR_Utterances(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Authorization") != "Token test-key" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "video/mp4" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "fake-video" {
			t.Errorf("body = %q", string(body))
		}

		json.NewEncoder(w).Encode(map[string]any{
			"results": map[string]any{
				"utterances": []map[string]any{
					{"start": 0.0, "end": 2.5, "transcript": "Hello world"},
					{"start": 3.0, "end": 5.0, "transcript": "  Buy now  "},
					{"start": 6.0, "end": 7.0, "transcript": "   "},
				},
			},
		})
	}))
	defer server.Close()

	old := deepgramBaseURL
	deepgramBaseURL = server.URL
	defer func() { deepgramBaseURL = old }()

	result, err := RunASR(context.Background(), []byte("fake-video"), "test-key")
	if err != nil {
		t.Fatalf("RunASR error: %v", err)
	}

	if len(result.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(result.Segments))
	}
	if result.Segments[0].Text != "Hello world" {
		t.Errorf("seg 0 = %q", result.Segments[0].Text)
	}
	if result.Segments[1].Text != "Buy now" {
		t.Errorf("seg 1 = %q", result.Segments[1].Text)
	}
}

func TestRunASR_FallbackToWords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// "now" ends at 4.5, 4.5 - 0.0 = 4.5 >= 3.0 → all words in one chunk
		json.NewEncoder(w).Encode(map[string]any{
			"results": map[string]any{
				"utterances": []any{},
				"channels": []map[string]any{
					{
						"alternatives": []map[string]any{
							{
								"words": []map[string]any{
									{"word": "Buy", "start": 0.0, "end": 0.5},
									{"word": "this", "start": 0.6, "end": 1.0},
									{"word": "product", "start": 1.5, "end": 2.0},
									{"word": "now", "start": 4.0, "end": 4.5},
									{"word": "and", "start": 5.0, "end": 5.2},
									{"word": "save", "start": 5.5, "end": 6.0},
								},
							},
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	old := deepgramBaseURL
	deepgramBaseURL = server.URL
	defer func() { deepgramBaseURL = old }()

	result, err := RunASR(context.Background(), []byte("video"), "key")
	if err != nil {
		t.Fatalf("RunASR error: %v", err)
	}

	// "now" ends at 4.5, 4.5 - 0.0 = 4.5 >= 3.0 → first chunk = "Buy this product now"
	// "save" ends at 6.0, 6.0 - 5.0 = 1.0 < 3.0 → flushed as remainder = "and save"
	if len(result.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(result.Segments))
	}
	if result.Segments[0].Text != "Buy this product now" {
		t.Errorf("seg 0 = %q, want %q", result.Segments[0].Text, "Buy this product now")
	}
	if result.Segments[1].Text != "and save" {
		t.Errorf("seg 1 = %q, want %q", result.Segments[1].Text, "and save")
	}
}

func TestRunASR_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"results": map[string]any{},
		})
	}))
	defer server.Close()

	old := deepgramBaseURL
	deepgramBaseURL = server.URL
	defer func() { deepgramBaseURL = old }()

	result, err := RunASR(context.Background(), []byte("video"), "key")
	if err != nil {
		t.Fatalf("RunASR error: %v", err)
	}
	if len(result.Segments) != 0 {
		t.Errorf("expected 0 segments, got %d", len(result.Segments))
	}
}

func TestRunASR_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	old := deepgramBaseURL
	deepgramBaseURL = server.URL
	defer func() { deepgramBaseURL = old }()

	_, err := RunASR(context.Background(), []byte("video"), "key")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
