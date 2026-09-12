package outbox

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"
)

const maxAcknowledgementBytes = 4096

// A transport success alone does not prove a side effect completed. Accept a
// bounded, complete JSON response; callers must check its event/object identity.
func decodeAcknowledgement(response *http.Response, target any) error {
	if response.StatusCode != http.StatusOK {
		return errors.New("completion acknowledgement requires HTTP 200")
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("completion acknowledgement requires application/json")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAcknowledgementBytes+1))
	if err != nil || len(body) > maxAcknowledgementBytes || !utf8.Valid(body) {
		return errors.New("completion acknowledgement body is invalid or too large")
	}
	if err := json.Unmarshal(body, target); err != nil {
		return errors.New("completion acknowledgement is not a complete JSON object")
	}
	return nil
}
