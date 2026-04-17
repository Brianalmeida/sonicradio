package metadata

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// IcyMetadata represents the parsed ICY metadata.
type IcyMetadata struct {
	StreamTitle string
	StreamUrl   string
}

// FetchIcyMetadata connects to the given URL and attempts to read ICY metadata.
// It returns a channel that emits metadata updates.
func FetchIcyMetadata(ctx context.Context, url string) (<-chan IcyMetadata, error) {
	out := make(chan IcyMetadata, 1)

	go func() {
		defer close(out)
		var lastTitle string
		backoff := time.Second

		for {
			select {
			case <-ctx.Done():
				return
			default:
				err := func() error {
					req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
					if err != nil {
						return err
					}

					req.Header.Set("Icy-MetaData", "1")
					req.Header.Set("User-Agent", "SonicRadio/1.0")

					client := &http.Client{
						Timeout: 30 * time.Second,
					}

					resp, err := client.Do(req)
					if err != nil {
						return err
					}
					defer resp.Body.Close()

					metaint := 0
					if val := resp.Header.Get("icy-metaint"); val != "" {
						fmt.Sscanf(val, "%d", &metaint)
					}

					if metaint <= 0 {
						return fmt.Errorf("no ICY metadata supported by station")
					}

					// Successfully connected, reset backoff
					backoff = time.Second
					reader := bufio.NewReader(resp.Body)

					for {
						select {
						case <-ctx.Done():
							return ctx.Err()
						default:
							// Skip music data
							_, err := io.CopyN(io.Discard, reader, int64(metaint))
							if err != nil {
								return err
							}

							// Read metadata length byte
							lengthByte, err := reader.ReadByte()
							if err != nil {
								return err
							}

							length := int(lengthByte) * 16
							if length <= 0 {
								continue
							}
							if length > 4096 {
								// Security: ignore overly large metadata chunks
								_, _ = io.CopyN(io.Discard, reader, int64(length))
								continue
							}

							// Read metadata content
							metaBytes := make([]byte, length)
							_, err = io.ReadFull(reader, metaBytes)
							if err != nil {
								return err
							}

							metaStr := string(metaBytes)
							if strings.Contains(metaStr, "StreamTitle='") {
								start := strings.Index(metaStr, "StreamTitle='") + len("StreamTitle='")
								end := strings.Index(metaStr[start:], "';")
								if end > 0 {
									title := strings.TrimSpace(metaStr[start : start+end])
									if len(title) > 255 {
										title = title[:255]
									}
									if title != lastTitle {
										select {
										case out <- IcyMetadata{StreamTitle: title}:
											lastTitle = title
										case <-ctx.Done():
											return ctx.Err()
										}
									}
								}
							}
						}
					}
				}()

				if err != nil {
					if errors.Is(err, context.Canceled) {
						return
					}
					// Apply exponential backoff before reconnecting
					select {
					case <-ctx.Done():
						return
					case <-time.After(backoff):
						backoff *= 2
						if backoff > 30*time.Second {
							backoff = 30 * time.Second
						}
					}
				}
			}
		}
	}()

	return out, nil
}
