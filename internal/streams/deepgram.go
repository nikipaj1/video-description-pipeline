package streams

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ASRResult is the output of the Deepgram transcription stream.
type ASRResult struct {
	Segments []ASRSegment `json:"segments"`
}

type ASRSegment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

type wordEntry struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// deepgramResponse represents the relevant parts of Deepgram's API response.
type deepgramResponse struct {
	Results struct {
		Utterances []struct {
			Start      float64 `json:"start"`
			End        float64 `json:"end"`
			Transcript string  `json:"transcript"`
		} `json:"utterances"`
		Channels []struct {
			Alternatives []struct {
				Words []wordEntry `json:"words"`
			} `json:"alternatives"`
		} `json:"channels"`
	} `json:"results"`
}

// deepgramBaseURL can be overridden in tests.
var deepgramBaseURL = "https://api.deepgram.com"

// RunASR sends video bytes to Deepgram Nova-3 pre-recorded API and returns
// timestamped transcript segments.
func RunASR(ctx context.Context, videoBytes []byte, apiKey string) (*ASRResult, error) {
	url := deepgramBaseURL + "/v1/listen?model=nova-3&smart_format=true&utterances=true&punctuate=true"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(videoBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Token "+apiKey)
	req.Header.Set("Content-Type", "video/mp4")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deepgram request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("deepgram returned %d: %s", resp.StatusCode, string(body))
	}

	var dgResp deepgramResponse
	if err := json.NewDecoder(resp.Body).Decode(&dgResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	result := &ASRResult{}

	// Primary: use utterances (sentence-level segments with timestamps)
	for _, u := range dgResp.Results.Utterances {
		text := strings.TrimSpace(u.Transcript)
		if text != "" {
			result.Segments = append(result.Segments, ASRSegment{
				Start: u.Start,
				End:   u.End,
				Text:  text,
			})
		}
	}

	// Fallback: if no utterances, group word-level results into ~3s chunks
	if len(result.Segments) == 0 && len(dgResp.Results.Channels) > 0 {
		alts := dgResp.Results.Channels[0].Alternatives
		if len(alts) > 0 {
			result.Segments = groupWordsIntoChunks(alts[0].Words, 3.0)
		}
	}

	return result, nil
}

func groupWordsIntoChunks(words []wordEntry, chunkDuration float64) []ASRSegment {
	var segments []ASRSegment
	var chunk []string
	var chunkStart float64
	started := false

	for _, w := range words {
		if !started {
			chunkStart = w.Start
			started = true
		}
		chunk = append(chunk, w.Word)

		if w.End-chunkStart >= chunkDuration {
			segments = append(segments, ASRSegment{
				Start: chunkStart,
				End:   w.End,
				Text:  strings.Join(chunk, " "),
			})
			chunk = nil
			started = false
		}
	}

	// Flush remaining
	if len(chunk) > 0 && len(words) > 0 {
		segments = append(segments, ASRSegment{
			Start: chunkStart,
			End:   words[len(words)-1].End,
			Text:  strings.Join(chunk, " "),
		})
	}

	return segments
}
