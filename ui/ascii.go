package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lucasb-eyer/go-colorful"
	"github.com/nfnt/resize"
)

var (
	logoCache = make(map[string]string)
	cacheMu   sync.RWMutex
	// httpClient can be overridden in tests
	httpClient = &http.Client{Timeout: 5 * time.Second}
)

func DefaultASCIIIcon() string {
	return `      .---------.
    .'  _   _    '.
    |  (o) (o)    |
    |   _   _     |
   /|  ( ) ( )    |\
  / |             | \
 /  '-------------'  \
 |   [=========]     |
 |    _   _   _      |
 |   ( ) ( ) ( )     |
 '-------------------'
    Radio Playing...`
}

// FetchAndConvertLogo fetches an image from the URL and converts it to high-detail ASCII (Braille).
func FetchAndConvertLogo(ctx context.Context, logoURL string) string {
	if logoURL == "" {
		return DefaultASCIIIcon()
	}

	// Security: Validate URL scheme
	if !strings.HasPrefix(logoURL, "http://") && !strings.HasPrefix(logoURL, "https://") {
		return DefaultASCIIIcon()
	}

	// Check cache
	cacheMu.RLock()
	if cached, ok := logoCache[logoURL]; ok {
		cacheMu.RUnlock()
		return cached
	}
	cacheMu.RUnlock()

	// Fetch image
	req, err := http.NewRequestWithContext(ctx, "GET", logoURL, nil)
	if err != nil {
		return DefaultASCIIIcon()
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return DefaultASCIIIcon()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return DefaultASCIIIcon()
	}

	// Security: Check Content-Type to ensure it's an image
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		return DefaultASCIIIcon()
	}

	// Security: Limit response size to 2MB
	const maxImageSize = 2 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImageSize))
	if err != nil || len(body) == 0 {
		return DefaultASCIIIcon()
	}

	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return DefaultASCIIIcon()
	}

	// Convert to high-detail Braille
	asciiStr := renderBraille(img, 42, 19)
	if asciiStr == "" {
		asciiStr = DefaultASCIIIcon()
	}

	// Save to cache
	cacheMu.Lock()
	if len(logoCache) >= 100 {
		logoCache = make(map[string]string)
	}
	logoCache[logoURL] = asciiStr
	cacheMu.Unlock()

	return asciiStr
}

func renderBraille(img image.Image, width, height int) string {
	// Braille is 2x4 dots per character.
	// So we need 2 * width and 4 * height pixels.
	img = resize.Resize(uint(width*2), uint(height*4), img, resize.Lanczos3)
	bounds := img.Bounds()

	// 4x4 Bayer matrix for ordered dithering
	bayer := [4][4]float64{
		{0.0625, 0.5625, 0.1875, 0.6875},
		{0.8125, 0.3125, 0.9375, 0.4375},
		{0.2500, 0.7500, 0.1250, 0.6250},
		{1.0000, 0.5000, 0.8750, 0.3750},
	}

	var sb strings.Builder
	for y := bounds.Min.Y; y < bounds.Max.Y; y += 4 {
		for x := bounds.Min.X; x < bounds.Max.X; x += 2 {
			var b rune = 0x2800
			var rSum, gSum, bSum float64
			count := 0

			// 2x4 dots mapping (Standard Braille)
			// Dot positions:
			// 1 4
			// 2 5
			// 3 6
			// 7 8
			// Bits:
			// 0 3
			// 1 4
			// 2 5
			// 6 7
			dots := [8][2]int{
				{0, 0}, {0, 1}, {0, 2},
				{1, 0}, {1, 1}, {1, 2},
				{0, 3}, {1, 3},
			}

			for i, dot := range dots {
				px := x + dot[0]
				py := y + dot[1]
				if px < bounds.Max.X && py < bounds.Max.Y {
					c, _ := colorful.MakeColor(img.At(px, py))
					_, _, l := c.Hsl()
					
					// Apply ordered dithering
					if l > bayer[px%4][py%4] {
						b |= (1 << uint(i))
					}
					
					rSum += c.R
					gSum += c.G
					bSum += c.B
					count++
				}
			}

			avgColor := colorful.Color{R: rSum / float64(count), G: gSum / float64(count), B: bSum / float64(count)}
			
			// Render the Braille char with the average color using TrueColor ANSI
			// We could optimize by only sending color if it changes, but TrueColor is usually fast enough
			// if it doesn't include the reset every time.
			sb.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm%c", 
				uint8(avgColor.R*255), uint8(avgColor.G*255), uint8(avgColor.B*255), b))
		}
		sb.WriteString("\x1b[0m\n")
	}

	return sb.String()
}
