package modelsdev

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

func Fetch(ctx context.Context, client *http.Client, url string) (Database, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if url == "" {
		url = DefaultURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models.dev payload: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fetch models.dev payload: HTTP %d: %s", resp.StatusCode, string(body))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return LoadBytes(data)
}
