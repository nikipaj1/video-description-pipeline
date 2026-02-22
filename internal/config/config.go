package config

import "os"

type Config struct {
	// R2 / S3
	R2EndpointURL    string
	R2AccessKeyID    string
	R2SecretAccessKey string
	R2Bucket         string

	// API keys
	DeepgramAPIKey string
	GeminiAPIKey   string

	// Server
	Port string
}

func Load() *Config {
	return &Config{
		R2EndpointURL:    getenv("R2_ENDPOINT_URL", ""),
		R2AccessKeyID:    getenv("R2_ACCESS_KEY_ID", ""),
		R2SecretAccessKey: getenv("R2_SECRET_ACCESS_KEY", ""),
		R2Bucket:         getenv("R2_BUCKET", "entropy-frames"),

		DeepgramAPIKey: getenv("DEEPGRAM_API_KEY", ""),
		GeminiAPIKey:   getenv("GEMINI_API_KEY", ""),

		Port: getenv("PORT", "8080"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
