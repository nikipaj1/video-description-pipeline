package r2

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Client struct {
	s3     *s3.Client
	bucket string
}

type KeyframeMeta struct {
	Index       int     `json:"index"`
	FrameNumber int     `json:"frame_number"`
	TimestampSec float64 `json:"timestamp_sec"`
	EntropyScore float64 `json:"entropy_score"`
	R2Key       string  `json:"r2_key"`
}

type KeyframeMetadataFile struct {
	Keyframes []KeyframeMeta `json:"keyframes"`
}

func NewClient(endpointURL, accessKeyID, secretAccessKey, bucket string) *Client {
	cfg := aws.Config{
		Region:      "auto",
		Credentials: credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""),
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
		o.BaseEndpoint = &endpointURL
	})

	return &Client{s3: client, bucket: bucket}
}

// DownloadVideo downloads the raw video bytes from R2.
func (c *Client) DownloadVideo(ctx context.Context, adID string) ([]byte, error) {
	key := fmt.Sprintf("ads/%s/video.mp4", adID)
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &c.bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, fmt.Errorf("download video %s: %w", key, err)
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

// DownloadKeyframeMetadata fetches the metadata.json written by entropy-frames-selector.
func (c *Client) DownloadKeyframeMetadata(ctx context.Context, adID string) ([]KeyframeMeta, error) {
	key := fmt.Sprintf("ads/%s/keyframes/metadata.json", adID)
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &c.bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, fmt.Errorf("download metadata %s: %w", key, err)
	}
	defer out.Body.Close()

	var meta KeyframeMetadataFile
	if err := json.NewDecoder(out.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("decode metadata: %w", err)
	}
	return meta.Keyframes, nil
}

// DownloadKeyframeImages downloads all keyframe JPEGs for an ad.
// Returns a map of r2_key -> image bytes.
func (c *Client) DownloadKeyframeImages(ctx context.Context, adID string, metas []KeyframeMeta) (map[string][]byte, error) {
	images := make(map[string][]byte, len(metas))
	for _, m := range metas {
		out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
			Bucket: &c.bucket,
			Key:    &m.R2Key,
		})
		if err != nil {
			return nil, fmt.Errorf("download keyframe %s: %w", m.R2Key, err)
		}
		data, err := io.ReadAll(out.Body)
		out.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read keyframe %s: %w", m.R2Key, err)
		}
		images[m.R2Key] = data
	}
	return images, nil
}

// ListKeyframeKeys lists all .jpg keys under ads/{adID}/keyframes/.
func (c *Client) ListKeyframeKeys(ctx context.Context, adID string) ([]string, error) {
	prefix := fmt.Sprintf("ads/%s/keyframes/", adID)
	out, err := c.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: &c.bucket,
		Prefix: &prefix,
	})
	if err != nil {
		return nil, fmt.Errorf("list keyframes: %w", err)
	}

	var keys []string
	for _, obj := range out.Contents {
		if strings.HasSuffix(*obj.Key, ".jpg") {
			keys = append(keys, *obj.Key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// UploadJSON uploads a JSON-serializable value to R2.
func (c *Client) UploadJSON(ctx context.Context, key string, data any) error {
	body, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	contentType := "application/json"
	_, err = c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &c.bucket,
		Key:         &key,
		Body:        bytes.NewReader(body),
		ContentType: &contentType,
	})
	if err != nil {
		return fmt.Errorf("upload %s: %w", key, err)
	}
	return nil
}
