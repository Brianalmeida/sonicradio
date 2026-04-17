package ui

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFetchAndConvertLogo_NetworkOnce(t *testing.T) {
	requestCount := 0

	// Create a dummy image to serve
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for x := 0; x < 10; x++ {
		for y := 0; y < 10; y++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "image/png")
		png.Encode(w, img)
	}))
	defer server.Close()

	// Override HTTP client for testing
	originalClient := httpClient
	httpClient = server.Client()
	defer func() { httpClient = originalClient }()

	// First request: should hit the server
	result1 := FetchAndConvertLogo(context.Background(), server.URL)
	assert.NotEqual(t, DefaultASCIIIcon(), result1, "Should return converted ASCII, not default icon")
	assert.Equal(t, 1, requestCount, "Server should have been hit exactly once")

	// Second request: should hit the cache
	result2 := FetchAndConvertLogo(context.Background(), server.URL)
	assert.Equal(t, result1, result2, "Cache result should match original result")
	assert.Equal(t, 1, requestCount, "Server should not be hit on the second request")
}

func TestFetchAndConvertLogo_InvalidURL(t *testing.T) {
	// Requesting an empty string should return default icon
	result := FetchAndConvertLogo(context.Background(), "")
	assert.Equal(t, DefaultASCIIIcon(), result)

	// Override HTTP client with a very short timeout for failure
	originalClient := httpClient
	httpClient = &http.Client{Timeout: 1 * time.Millisecond}
	defer func() { httpClient = originalClient }()

	result2 := FetchAndConvertLogo(context.Background(), "http://255.255.255.255/invalid")
	assert.Equal(t, DefaultASCIIIcon(), result2)
}
