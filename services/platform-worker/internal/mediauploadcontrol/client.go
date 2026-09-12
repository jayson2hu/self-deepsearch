// Package mediauploadcontrol controls the single Beijing upload executor.
// It does not clear usage latches, pending upload journals, or public caches.
package mediauploadcontrol

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalid   = errors.New("media_upload_control_invalid")
	ErrConflict  = errors.New("media_upload_control_conflict")
	ErrBusy      = errors.New("media_upload_control_busy")
	ErrUncertain = errors.New("media_upload_control_not_confirmed")
	uuidPattern  = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)
)

const maxGeneration = 1<<53 - 1

type Command struct {
	ID                 string `json:"command_id"`
	ExpectedEpoch      string `json:"expected_epoch"`
	ExpectedGeneration int64  `json:"expected_generation"`
	Mode               string `json:"mode"`
	Reason             string `json:"reason"`
	ResumeConfirmed    bool   `json:"resume_confirmed"`
	IssuedAt           string `json:"issued_at,omitempty"`
	ExpiresAt          string `json:"expires_at,omitempty"`
}

func (c Command) valid() bool {
	if c.IssuedAt != "" || c.ExpiresAt != "" {
		if _, _, ok := c.Window(); !ok {
			return false
		}
	}
	if !uuidPattern.MatchString(c.ID) || c.ExpectedGeneration < 0 || c.ExpectedGeneration >= maxGeneration ||
		(c.ExpectedGeneration == 0 && c.ExpectedEpoch != "") || (c.ExpectedGeneration > 0 && !uuidPattern.MatchString(c.ExpectedEpoch)) {
		return false
	}
	if (c.Mode != "paused" && c.Mode != "enabled") || (c.Mode == "enabled") != c.ResumeConfirmed || (c.ExpectedGeneration == 0 && c.Mode != "paused") {
		return false
	}
	if !utf8.ValidString(c.Reason) || c.Reason != strings.TrimSpace(c.Reason) || utf8.RuneCountInString(c.Reason) < 2 || utf8.RuneCountInString(c.Reason) > 1000 {
		return false
	}
	return strings.IndexFunc(c.Reason, func(r rune) bool { return r < 32 || unicode.Is(unicode.Cs, r) }) == -1
}

// Window is mandatory for queued commands. Legacy one-shot operator commands
// retain their original wire/ledger format during the incremental upgrade.
func (c Command) Window() (time.Time, time.Time, bool) {
	const layout = "2006-01-02T15:04:05Z"
	issued, a := time.Parse(layout, c.IssuedAt)
	expires, b := time.Parse(layout, c.ExpiresAt)
	ok := a == nil && b == nil && issued.Format(layout) == c.IssuedAt && expires.Format(layout) == c.ExpiresAt &&
		expires.After(issued) && expires.Sub(issued) <= 5*time.Minute
	return issued, expires, ok
}

// Matches reports only exact command/generation application, including a
// later authenticated status lookup after a lost response.
func (c Command) Matches(r Receipt) bool {
	epoch := c.ExpectedEpoch
	if epoch == "" {
		epoch = c.ID
	}
	return r.Epoch == epoch && r.Generation == c.ExpectedGeneration+1 && r.CommandID == c.ID && r.Mode == c.Mode
}

type Receipt struct {
	Status     string `json:"status"`
	Epoch      string `json:"epoch"`
	Generation int64  `json:"generation"`
	Mode       string `json:"mode"`
	CommandID  string `json:"command_id"`
	AppliedAt  string `json:"applied_at"`
}

type Client struct {
	origin string
	secret []byte
	http   *http.Client
	now    func() time.Time
}

func NewClient(origin, secret string, allowHTTP bool, client *http.Client) (*Client, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		u.Opaque != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && !(u.Scheme == "http" && allowHTTP)) || len(strings.TrimSpace(secret)) < 32 {
		return nil, ErrInvalid
	}
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = nil
		client = &http.Client{Timeout: 8 * time.Second, Transport: transport}
	}
	copy := *client
	copy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{origin: strings.TrimSuffix(origin, "/"), secret: []byte(strings.TrimSpace(secret)), http: &copy, now: time.Now}, nil
}

func (c *Client) Status(ctx context.Context) (Receipt, error) {
	r, err := c.send(ctx, "/v1/upload-control/status", []byte(`{}`))
	if err == nil && r.Status != "observed" {
		err = ErrUncertain
	}
	if err != nil {
		return Receipt{}, err
	}
	return r, nil
}

func (c *Client) Apply(ctx context.Context, command Command) (Receipt, error) {
	if !command.valid() {
		return Receipt{}, ErrInvalid
	}
	body, err := json.Marshal(command)
	if err != nil {
		return Receipt{}, ErrInvalid
	}
	r, err := c.send(ctx, "/v1/upload-control/apply", body)
	if err != nil {
		return Receipt{}, err
	}
	epoch := command.ExpectedEpoch
	if epoch == "" {
		epoch = command.ID
	}
	if (r.Status != "applied" && r.Status != "replayed") || r.CommandID != command.ID || r.Generation != command.ExpectedGeneration+1 || r.Mode != command.Mode || r.Epoch != epoch {
		return Receipt{}, ErrUncertain
	}
	return r, nil
}

func mac(secret []byte, prefix string, body []byte) string {
	h := hmac.New(sha256.New, secret)
	_, _ = h.Write([]byte(prefix))
	_, _ = h.Write(body)
	return "u1=" + hex.EncodeToString(h.Sum(nil))
}

func requestPrefix(path, timestamp, nonce string) string {
	return "sd-upload-control-request-v1\nPOST\n" + path + "\n" + timestamp + "\n" + nonce + "\n"
}

func responsePrefix(path, timestamp, nonce string, body []byte, status int) string {
	digest := sha256.Sum256(body)
	return "sd-upload-control-response-v1\nPOST\n" + path + "\n" + timestamp + "\n" + nonce + "\n" + hex.EncodeToString(digest[:]) + "\n" + strconv.Itoa(status) + "\n"
}

func (c *Client) send(ctx context.Context, path string, body []byte) (Receipt, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Receipt{}, ErrUncertain
	}
	nonce := hex.EncodeToString(random[:])
	timestamp := strconv.FormatInt(c.now().UTC().Unix(), 10)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.origin+path, bytes.NewReader(body))
	if err != nil {
		return Receipt{}, ErrInvalid
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-SD-Control-Timestamp", timestamp)
	request.Header.Set("X-SD-Control-Nonce", nonce)
	request.Header.Set("X-SD-Control-Signature", mac(c.secret, requestPrefix(path, timestamp, nonce), body))
	response, err := c.http.Do(request)
	if err != nil {
		return Receipt{}, ErrUncertain
	}
	defer response.Body.Close()
	ack, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(ack) > 4096 || !utf8.Valid(ack) || len(response.Header.Values("Content-Type")) != 1 ||
		response.Header.Get("Content-Type") != "application/json" || response.Header.Get("Content-Encoding") != "" || len(response.Header.Values("X-SD-Control-Response")) != 1 {
		return Receipt{}, ErrUncertain
	}
	expected := mac(c.secret, responsePrefix(path, timestamp, nonce, body, response.StatusCode), ack)
	if !hmac.Equal([]byte(expected), []byte(response.Header.Get("X-SD-Control-Response"))) {
		return Receipt{}, ErrUncertain
	}
	if response.StatusCode == http.StatusConflict {
		return Receipt{}, ErrConflict
	}
	if response.StatusCode == http.StatusServiceUnavailable {
		return Receipt{}, ErrBusy
	}
	if response.StatusCode != http.StatusOK || !uniqueReceiptFields(ack) {
		return Receipt{}, ErrUncertain
	}
	var receipt Receipt
	if json.Unmarshal(ack, &receipt) != nil || !receipt.valid(c.now()) {
		return Receipt{}, ErrUncertain
	}
	return receipt, nil
}

func (r Receipt) valid(now time.Time) bool {
	if r.Status != "observed" && r.Status != "applied" && r.Status != "replayed" {
		return false
	}
	if r.Generation == 0 {
		return r.Status == "observed" && r.Mode == "paused" && r.Epoch == "" && r.CommandID == "" && r.AppliedAt == ""
	}
	at, err := time.Parse("2006-01-02T15:04:05Z", r.AppliedAt)
	return r.Generation > 0 && r.Generation <= maxGeneration && uuidPattern.MatchString(r.Epoch) && uuidPattern.MatchString(r.CommandID) &&
		(r.Mode == "paused" || r.Mode == "enabled") && err == nil && at.Format("2006-01-02T15:04:05Z") == r.AppliedAt && !at.After(now.Add(300*time.Second))
}

// Do not allow duplicate/missing/extra JSON fields to change receipt meaning.
func uniqueReceiptFields(body []byte) bool {
	d := json.NewDecoder(bytes.NewReader(body))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	fields := map[string]bool{"status": false, "epoch": false, "generation": false, "mode": false, "command_id": false, "applied_at": false}
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return false
		}
		key, ok := token.(string)
		seen, exists := fields[key]
		if !ok || !exists || seen {
			return false
		}
		fields[key] = true
		var raw json.RawMessage
		if d.Decode(&raw) != nil || bytes.Equal(raw, []byte("null")) {
			return false
		}
	}
	if _, err := d.Token(); err != nil {
		return false
	}
	if _, err := d.Token(); err != io.EOF {
		return false
	}
	for _, seen := range fields {
		if !seen {
			return false
		}
	}
	return true
}

func writeError(stderr io.Writer, err error) {
	fmt.Fprintln(stderr, err.Error()+"; no remote success confirmed; inspect status or retry the identical command id and payload")
}
