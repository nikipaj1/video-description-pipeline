package streams

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// VLMResult is the output of the Gemini VLM description stream.
type VLMResult struct {
	Frames []VLMFrame `json:"frames"`
}

type VLMFrame struct {
	FrameIndex   int     `json:"frame_index"`
	TimestampSec float64 `json:"timestamp_sec"`
	Description  string  `json:"description"`
}

const vlmPromptTemplate = `Analyze this frame from a video advertisement.
Previous frame context: %s
Timestamp: %.1fs

Describe in 2-3 sentences covering:
1. What is happening visually (people, product, setting, action)
2. Camera movement and shot type (close-up, wide shot, zoom in, pan, cut, handheld shake, tracking)
3. Emotional tone, color palette, pacing feel
4. Any motion blur, fast cuts, slow motion, or speed ramp effects

Be specific and concrete. Use explicit motion vocabulary: cut, zoom, pan, handheld, slow motion, fast cut, tracking shot, static shot, dolly, whip pan.`

// KeyframeInput represents a keyframe with its metadata and image bytes.
type KeyframeInput struct {
	FrameIndex   int
	TimestampSec float64
	ImageBytes   []byte // JPEG bytes
}

// RunVLM generates visual descriptions for each keyframe via Gemini 2.0 Flash.
// Sequential per-frame: each prompt includes previous frame's description for continuity.
func RunVLM(ctx context.Context, keyframes []KeyframeInput, apiKey string) (*VLMResult, error) {
	result := &VLMResult{}
	prevDesc := "This is the first frame of the ad."

	for _, kf := range keyframes {
		prompt := fmt.Sprintf(vlmPromptTemplate, prevDesc, kf.TimestampSec)

		desc, err := callGemini(ctx, apiKey, kf.ImageBytes, prompt)
		if err != nil {
			desc = fmt.Sprintf("[Error: %v]", err)
		}

		result.Frames = append(result.Frames, VLMFrame{
			FrameIndex:   kf.FrameIndex,
			TimestampSec: kf.TimestampSec,
			Description:  desc,
		})
		if err == nil {
			prevDesc = desc
		}
	}

	return result, nil
}

// geminiRequest is the Gemini REST API request body.
type geminiRequest struct {
	Contents []geminiContent `json:"contents"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text       string          `json:"text,omitempty"`
	InlineData *geminiInline   `json:"inline_data,omitempty"`
}

type geminiInline struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"` // base64
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// geminiBaseURL can be overridden in tests.
var geminiBaseURL = "https://generativelanguage.googleapis.com"

func callGemini(ctx context.Context, apiKey string, imageBytes []byte, prompt string) (string, error) {
	url := fmt.Sprintf(
		"%s/v1beta/models/gemini-2.0-flash:generateContent?key=%s",
		geminiBaseURL, apiKey,
	)

	reqBody := geminiRequest{
		Contents: []geminiContent{{
			Parts: []geminiPart{
				{Text: prompt},
				{InlineData: &geminiInline{
					MimeType: "image/jpeg",
					Data:     base64.StdEncoding.EncodeToString(imageBytes),
				}},
			},
		}},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini returned %d: %s", resp.StatusCode, string(respBody))
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(respBody, &gemResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if gemResp.Error != nil {
		return "", fmt.Errorf("gemini error: %s", gemResp.Error.Message)
	}

	if len(gemResp.Candidates) == 0 || len(gemResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from gemini")
	}

	return strings.TrimSpace(gemResp.Candidates[0].Content.Parts[0].Text), nil
}
