package main

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"fiatjaf.com/croissant/global"
)

const maxFaviconSize = 2 << 20

var faviconStore = &faviconCache{
	client: &http.Client{Timeout: 10 * time.Second},
}

type faviconCache struct {
	mu              sync.Mutex
	url             string
	data            []byte
	contentType     string
	logoData        []byte
	logoContentType string
	client          *http.Client
}

func faviconHandler(w http.ResponseWriter, r *http.Request) {
	iconURL := global.S.RelayIcon

	data, contentType := faviconStore.get(iconURL)
	if len(data) == 0 {
		http.NotFound(w, r)
		return
	}

	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(data)
}

func (c *faviconCache) get(iconURL string) ([]byte, string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if iconURL == "" {
		return c.logo()
	}

	if iconURL == c.url && len(c.data) > 0 {
		return c.data, c.contentType
	}

	data, contentType, err := c.fetch(iconURL)
	if err != nil {
		L.Warn().Err(err).Msg("failed to fetch relay icon")
		return c.logo()
	}

	c.url = iconURL
	c.data = data
	c.contentType = contentType

	return data, contentType
}

func (c *faviconCache) logo() ([]byte, string) {
	if len(c.logoData) > 0 {
		return c.logoData, c.logoContentType
	}

	data, err := staticFiles.ReadFile("static/logo.png")
	if err != nil {
		L.Warn().Err(err).Msg("failed to read embedded logo")
		return nil, ""
	}

	c.logoData = data
	c.logoContentType = http.DetectContentType(data)

	return c.logoData, c.logoContentType
}

func (c *faviconCache) fetch(iconURL string) ([]byte, string, error) {
	req, err := http.NewRequest("GET", iconURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("invalid icon URL: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch icon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("unexpected status: %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFaviconSize))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read icon: %w", err)
	}
	if len(body) == 0 {
		return nil, "", fmt.Errorf("empty icon response")
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(body)
	}

	return body, contentType, nil
}
