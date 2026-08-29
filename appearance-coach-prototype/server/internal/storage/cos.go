package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	cos "github.com/tencentyun/cos-go-sdk-v5"
)

type COSConfig struct {
	BucketURL  string
	SecretID   string
	SecretKey  string
	KeyPrefix  string
	HTTPClient *http.Client
}

type COS struct {
	client    *cos.Client
	secretID  string
	secretKey string
	prefix    string
}

func NewCOS(cfg COSConfig) (*COS, error) {
	bucketURL, err := url.Parse(strings.TrimSpace(cfg.BucketURL))
	if err != nil || bucketURL.Scheme != "https" || bucketURL.Host == "" {
		return nil, fmt.Errorf("COS bucket URL must be an absolute HTTPS URL")
	}
	if strings.TrimSpace(cfg.SecretID) == "" || strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, fmt.Errorf("COS secret id and key are required")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Transport: &cos.AuthorizationTransport{
			SecretID: strings.TrimSpace(cfg.SecretID), SecretKey: strings.TrimSpace(cfg.SecretKey),
		}}
	}
	return &COS{
		client:   cos.NewClient(&cos.BaseURL{BucketURL: bucketURL}, httpClient),
		secretID: strings.TrimSpace(cfg.SecretID), secretKey: strings.TrimSpace(cfg.SecretKey),
		prefix: strings.Trim(strings.TrimSpace(cfg.KeyPrefix), "/"),
	}, nil
}

func (c *COS) Save(ctx context.Context, key string, reader io.Reader) (string, error) {
	key, err := c.objectKey(key)
	if err != nil {
		return "", err
	}
	response, err := c.client.Object.Put(ctx, key, reader, nil)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		return "", fmt.Errorf("save COS object: %w", err)
	}
	return key, nil
}

func (c *COS) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	key, err := c.objectKey(key)
	if err != nil {
		return nil, err
	}
	response, err := c.client.Object.Get(ctx, key, nil)
	if err != nil {
		return nil, fmt.Errorf("open COS object: %w", err)
	}
	return response.Body, nil
}

func (c *COS) Delete(ctx context.Context, key string) error {
	key, err := c.objectKey(key)
	if err != nil {
		return err
	}
	response, err := c.client.Object.Delete(ctx, key)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		return fmt.Errorf("delete COS object: %w", err)
	}
	return nil
}

func (c *COS) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	key, err := c.objectKey(key)
	if err != nil {
		return "", err
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	value, err := c.client.Object.GetPresignedURL(ctx, http.MethodGet, key, c.secretID, c.secretKey, ttl, nil)
	if err != nil {
		return "", fmt.Errorf("sign COS object URL: %w", err)
	}
	return value.String(), nil
}

func (c *COS) objectKey(value string) (string, error) {
	clean := path.Clean(strings.TrimPrefix(strings.TrimSpace(value), "/"))
	if clean == "." || clean == "" || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("invalid storage key")
	}
	if c.prefix == "" || clean == c.prefix || strings.HasPrefix(clean, c.prefix+"/") {
		return clean, nil
	}
	return c.prefix + "/" + clean, nil
}
