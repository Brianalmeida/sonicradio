package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

var (
	artworkCache = make(map[string]string)
	artworkMu    sync.RWMutex
	httpClient   = &http.Client{Timeout: 5 * time.Second}
)

type itunesResponse struct {
	ResultCount int `json:"resultCount"`
	Results     []struct {
		ArtworkUrl100 string `json:"artworkUrl100"`
		ArtworkUrl600 string `json:"artworkUrl600"`
	} `json:"results"`
}

// FetchArtworkURL queries the iTunes Search API for artwork based on a search term.
func FetchArtworkURL(ctx context.Context, term string) (string, error) {
	if term == "" {
		return "", nil
	}

	artworkMu.RLock()
	if cached, ok := artworkCache[term]; ok {
		artworkMu.RUnlock()
		return cached, nil
	}
	artworkMu.RUnlock()

	searchURL := fmt.Sprintf("https://itunes.apple.com/search?term=%s&entity=song&limit=1", url.QueryEscape(term))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("itunes api returned status %d", resp.StatusCode)
	}

	var itunesResp itunesResponse
	if err := json.NewDecoder(resp.Body).Decode(&itunesResp); err != nil {
		return "", err
	}

	if itunesResp.ResultCount == 0 || len(itunesResp.Results) == 0 {
		return "", nil
	}

	artworkURL := itunesResp.Results[0].ArtworkUrl600
	if artworkURL == "" {
		artworkURL = itunesResp.Results[0].ArtworkUrl100
	}

	if artworkURL != "" {
		artworkMu.Lock()
		if len(artworkCache) >= 100 {
			artworkCache = make(map[string]string)
		}
		artworkCache[term] = artworkURL
		artworkMu.Unlock()
	}

	return artworkURL, nil
}
