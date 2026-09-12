package mediainspect

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// These are release asset contracts, not configurable/user-provided URLs.
// Tests compare the hashes with display-web/public; update both in one release
// when intentionally changing a default. Canonical LF supports Windows builds.
var defaultAssets = [...]struct{ Path, SHA256 string }{
	{"/default-work.svg", "7edf44259fad59e040176f28922ec4f18364bb564330e47d2def6766d00d26c3"},
	{"/default-performer.svg", "4a0f9080f5ae7f7d4cc05b7c7f9c9fe7a6db0d79caa22f92da2541c2cba24ceb"},
}

type DefaultResult struct {
	Path       string `json:"path"`
	HTTPStatus int    `json:"http_status"`
	ErrorCode  string `json:"error_code"`
}

type HTTPDefaults struct {
	origin string
	client *http.Client
}

func NewHTTPDefaults(origin string, client *http.Client) (*HTTPDefaults, error) {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery ||
		u.Fragment != "" || strings.Contains(origin, "#") || len(origin) > 2048 {
		return nil, errors.New("MEDIA_INSPECT_DISPLAY_ORIGIN must be a configured HTTP(S) origin without credentials, path, query or fragment")
	}
	if client == nil {
		client = &http.Client{}
	}
	copy := *client
	// Enforce bounds even on an injected client. No cookies, redirects or
	// credentials; a login/challenge page must fail, not become a false pass.
	copy.Timeout = 5 * time.Second
	copy.Jar = nil
	copy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTPDefaults{origin: strings.TrimSuffix(u.String(), "/"), client: &copy}, nil
}

func (c *HTTPDefaults) Origin() string { return c.origin }

func (c *HTTPDefaults) Check(ctx context.Context) []DefaultResult {
	results := make([]DefaultResult, 0, len(defaultAssets))
	for _, asset := range defaultAssets {
		results = append(results, c.check(ctx, asset.Path, asset.SHA256))
	}
	return results
}

func (c *HTTPDefaults) check(ctx context.Context, path, expectedHash string) DefaultResult {
	r := DefaultResult{Path: path}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.origin+path, nil)
	if err != nil {
		r.ErrorCode = "request_failed"
		return r
	}
	request.Header.Set("Accept", "image/svg+xml")
	request.Header.Set("Cache-Control", "no-cache")
	response, err := c.client.Do(request)
	if err != nil {
		r.ErrorCode = "request_failed"
		return r
	}
	defer response.Body.Close()
	r.HTTPStatus = response.StatusCode
	if response.StatusCode != http.StatusOK {
		r.ErrorCode = "http_status"
		return r
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "image/svg+xml" {
		r.ErrorCode = "content_type"
		return r
	}
	const maximumBody = 64 * 1024
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumBody+1))
	if err != nil || len(body) > maximumBody {
		r.ErrorCode = "body_invalid"
		return r
	}
	digest := sha256.Sum256(bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n")))
	if hex.EncodeToString(digest[:]) != expectedHash {
		r.ErrorCode = "content_mismatch"
	}
	return r
}

func validDefaultResults(results []DefaultResult) bool {
	if len(results) != len(defaultAssets) {
		return false
	}
	for i, result := range results {
		if result.Path != defaultAssets[i].Path || result.HTTPStatus < 0 || result.HTTPStatus > 599 {
			return false
		}
		switch result.ErrorCode {
		case "":
			if result.HTTPStatus != http.StatusOK {
				return false
			}
		case "request_failed":
			if result.HTTPStatus != 0 {
				return false
			}
		case "http_status":
			if result.HTTPStatus < 100 || result.HTTPStatus == http.StatusOK {
				return false
			}
		case "content_type", "body_invalid", "content_mismatch":
			if result.HTTPStatus != http.StatusOK {
				return false
			}
		default:
			return false
		}
	}
	return true
}
