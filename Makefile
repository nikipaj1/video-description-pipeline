-include .env
export

IMAGE_NAME ?= $(DOCKERHUB_USER)/video-description-pipeline
TAG        ?= latest

.PHONY: build run docker-build docker-push docker-run test-health test-extract

build:
	go build -o bin/server ./cmd/server

run:
	go run ./cmd/server

docker-build:
	docker build -t $(IMAGE_NAME):$(TAG) .

docker-push:
	docker push $(IMAGE_NAME):$(TAG)

docker-run:
	docker run --rm -p 8080:8080 --env-file .env $(IMAGE_NAME):$(TAG)

AD_ID ?= test-ad
HOST  ?= localhost:8080

test-health:
	curl -sf "http://$(HOST)/health" | jq .

test-extract:
	curl -sf "http://$(HOST)/extract" \
	  -H "Content-Type: application/json" \
	  -d '{"ad_id": "$(AD_ID)"}' | jq .
