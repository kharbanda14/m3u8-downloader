package utils

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"
)

// UserAgent is the default user agent string used for HTTP requests
const UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"

// GetBaseURL extracts the base URL from a full URL string
func GetBaseURL(urlStr string) (string, error) {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return "", err
	}

	// Remove the filename part
	dir, _ := path.Split(parsedURL.Path)
	parsedURL.Path = dir

	return parsedURL.String(), nil
}

// FetchURL retrieves content from a URL with timeout
func FetchURL(urlStr string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", UserAgent)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(body), nil
}

// ResolveURL resolves a relative URL against a base URL
func ResolveURL(baseURL, relURL string) string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return relURL
	}

	rel, err := url.Parse(relURL)
	if err != nil {
		return relURL
	}

	resolvedURL := base.ResolveReference(rel)
	return resolvedURL.String()
}
