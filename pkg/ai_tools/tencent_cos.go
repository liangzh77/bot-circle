package ai_tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tencentyun/cos-go-sdk-v5"
)

type TencentCOSCredentials struct {
	SecretID  string `json:"secretId"`
	SecretKey string `json:"secretKey"`
}

type TencentCOSClient struct {
	BucketURL   string
	Credentials TencentCOSCredentials
	HTTPClient  *http.Client
}

type COSUploadRequest struct {
	LocalPath   string
	ObjectKey   string
	ContentType string
}

type COSUploadResult struct {
	BucketURL string
	ObjectKey string
	ETag      string
	URL       string
}

type COSSignedURLRequest struct {
	ObjectKey string
	Method    string
	Expires   time.Duration
}

type COSSignedURLResult struct {
	ObjectKey string
	URL       string
	Expires   time.Duration
}

type COSDeleteRequest struct {
	ObjectKey string
}

func ParseTencentCOSCredentials(raw string) (TencentCOSCredentials, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return TencentCOSCredentials{}, fmt.Errorf("tencent cos credentials are required")
	}
	var parsed TencentCOSCredentials
	if strings.HasPrefix(raw, "{") {
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return TencentCOSCredentials{}, fmt.Errorf("parse tencent cos credentials: %w", err)
		}
	} else if parts := strings.SplitN(raw, ":", 2); len(parts) == 2 {
		parsed.SecretID = strings.TrimSpace(parts[0])
		parsed.SecretKey = strings.TrimSpace(parts[1])
	} else {
		return TencentCOSCredentials{}, fmt.Errorf("tencent cos credentials must be JSON {secretId,secretKey} or secretId:secretKey")
	}
	if parsed.SecretID == "" || parsed.SecretKey == "" {
		return TencentCOSCredentials{}, fmt.Errorf("tencent cos secretId and secretKey are required")
	}
	return parsed, nil
}

func (c TencentCOSClient) UploadFile(ctx context.Context, input COSUploadRequest) (COSUploadResult, error) {
	if c.BucketURL == "" {
		return COSUploadResult{}, fmt.Errorf("tencent cos bucket url is required")
	}
	if c.Credentials.SecretID == "" || c.Credentials.SecretKey == "" {
		return COSUploadResult{}, fmt.Errorf("tencent cos credentials are required")
	}
	if input.LocalPath == "" {
		return COSUploadResult{}, fmt.Errorf("local path is required")
	}
	if input.ObjectKey == "" {
		input.ObjectKey = "techprobe/" + time.Now().UTC().Format("20060102T150405Z") + "/" + filepath.Base(input.LocalPath)
	}

	bucketURL, err := url.Parse(c.BucketURL)
	if err != nil {
		return COSUploadResult{}, err
	}
	base := &cos.BaseURL{BucketURL: bucketURL}
	client := cos.NewClient(base, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  c.Credentials.SecretID,
			SecretKey: c.Credentials.SecretKey,
			Transport: c.HTTPClientTransport(),
		},
	})

	file, err := os.Open(input.LocalPath)
	if err != nil {
		return COSUploadResult{}, err
	}
	defer file.Close()

	options := &cos.ObjectPutOptions{}
	if input.ContentType != "" {
		options.ObjectPutHeaderOptions = &cos.ObjectPutHeaderOptions{ContentType: input.ContentType}
	}
	resp, err := client.Object.Put(ctx, input.ObjectKey, file, options)
	if err != nil {
		return COSUploadResult{}, err
	}
	defer resp.Body.Close()

	return COSUploadResult{
		BucketURL: strings.TrimRight(c.BucketURL, "/"),
		ObjectKey: input.ObjectKey,
		ETag:      strings.Trim(resp.Header.Get("ETag"), "\""),
		URL:       strings.TrimRight(c.BucketURL, "/") + "/" + strings.TrimLeft(input.ObjectKey, "/"),
	}, nil
}

func (c TencentCOSClient) SignedURL(input COSSignedURLRequest) (COSSignedURLResult, error) {
	if c.BucketURL == "" {
		return COSSignedURLResult{}, fmt.Errorf("tencent cos bucket url is required")
	}
	if c.Credentials.SecretID == "" || c.Credentials.SecretKey == "" {
		return COSSignedURLResult{}, fmt.Errorf("tencent cos credentials are required")
	}
	if input.ObjectKey == "" {
		return COSSignedURLResult{}, fmt.Errorf("object key is required")
	}
	if input.Method == "" {
		input.Method = http.MethodGet
	}
	if input.Expires <= 0 {
		input.Expires = 30 * time.Minute
	}

	bucketURL, err := url.Parse(c.BucketURL)
	if err != nil {
		return COSSignedURLResult{}, err
	}
	client := cos.NewClient(&cos.BaseURL{BucketURL: bucketURL}, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  c.Credentials.SecretID,
			SecretKey: c.Credentials.SecretKey,
			Transport: c.HTTPClientTransport(),
		},
	})
	signedURL, err := client.Object.GetPresignedURL(context.Background(), input.Method, input.ObjectKey, c.Credentials.SecretID, c.Credentials.SecretKey, input.Expires, nil)
	if err != nil {
		return COSSignedURLResult{}, err
	}
	return COSSignedURLResult{
		ObjectKey: input.ObjectKey,
		URL:       signedURL.String(),
		Expires:   input.Expires,
	}, nil
}

func (c TencentCOSClient) DeleteObject(ctx context.Context, input COSDeleteRequest) error {
	if c.BucketURL == "" {
		return fmt.Errorf("tencent cos bucket url is required")
	}
	if c.Credentials.SecretID == "" || c.Credentials.SecretKey == "" {
		return fmt.Errorf("tencent cos credentials are required")
	}
	if input.ObjectKey == "" {
		return fmt.Errorf("object key is required")
	}

	bucketURL, err := url.Parse(c.BucketURL)
	if err != nil {
		return err
	}
	client := cos.NewClient(&cos.BaseURL{BucketURL: bucketURL}, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  c.Credentials.SecretID,
			SecretKey: c.Credentials.SecretKey,
			Transport: c.HTTPClientTransport(),
		},
	})
	_, err = client.Object.Delete(ctx, input.ObjectKey)
	return err
}

func (c TencentCOSClient) HTTPClientTransport() http.RoundTripper {
	if c.HTTPClient != nil && c.HTTPClient.Transport != nil {
		return c.HTTPClient.Transport
	}
	return http.DefaultTransport
}
