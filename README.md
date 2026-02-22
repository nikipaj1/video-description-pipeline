# video-description-pipeline

CPU-side extraction service for the ad creative platform. Runs Deepgram ASR and Gemini VLM descriptions on ad videos and keyframes from R2.

## Architecture

This Go service handles the two API-call-based extraction streams:
- **Deepgram Nova-3 ASR** — transcribes speech from the raw video
- **Gemini 2.0 Flash VLM** — generates visual descriptions per keyframe

The GPU-side streams (CLIP, GroundingDINO, PaddleOCR) run in `entropy-frames-selector`.

## Endpoints

- `GET /health` — service status and configured streams
- `POST /extract` — run extraction for an ad (`{"ad_id": "..."}`)

## Quick start

```bash
cp .env.example .env  # fill in R2 creds + API keys
make run              # local development
make test-health      # verify service is up
make test-extract AD_ID=test-ad
```

## Docker

```bash
make docker-build
make docker-run
```

Final image: ~20MB (multi-stage Alpine build).
