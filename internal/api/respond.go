// Package api implements the HTTP surface of the media server.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// APIError is the single error shape every failing endpoint returns, so the frontend
// only ever has to parse one thing.
type APIError struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details string `json:"details,omitempty"`
}

// writeJSON encodes v as the response body with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status line is already sent, so all we can do is record it.
		log.Printf("api: encode response: %v", err)
	}
}

// writeError sends a structured error. The message is intended to be shown to the user.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, APIError{Error: msg})
}

// writeErrorCode sends a structured error carrying a machine-readable code, used where
// the frontend needs to branch (for example on SETUP_REQUIRED).
func writeErrorCode(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, APIError{Error: msg, Code: code})
}

// decodeJSON reads a JSON request body into dst, rejecting unknown fields and oversized
// payloads. It writes the error response itself and reports whether decoding succeeded.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	const maxBody = 1 << 20 // 1 MiB is far more than any request here needs
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		var maxErr *http.MaxBytesError

		switch {
		case errors.As(err, &syntaxErr):
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("Malformed JSON at position %d.", syntaxErr.Offset))
		case errors.As(err, &typeErr):
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("Field %q has the wrong type.", typeErr.Field))
		case errors.As(err, &maxErr):
			writeError(w, http.StatusRequestEntityTooLarge, "Request body is too large.")
		case errors.Is(err, io.EOF):
			writeError(w, http.StatusBadRequest, "Request body is empty.")
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			field := strings.TrimPrefix(err.Error(), "json: unknown field ")
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Unknown field %s.", field))
		default:
			writeError(w, http.StatusBadRequest, "Could not parse the request body.")
		}
		return false
	}

	// A second value in the body means the client sent something unexpected.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Request body must contain a single JSON object.")
		return false
	}
	return true
}

// queryInt reads an integer query parameter, falling back to def when absent or invalid.
func queryInt(r *http.Request, key string, def int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

// queryIntClamped reads an integer query parameter and clamps it into [min, max].
func queryIntClamped(r *http.Request, key string, def, min, max int) int {
	n := queryInt(r, key, def)
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

// queryBool reads a boolean query parameter ("1", "true", "yes" are all true).
func queryBool(r *http.Request, key string) bool {
	switch strings.ToLower(r.URL.Query().Get(key)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// Page is the envelope for every paginated list endpoint.
type Page[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

// paginate slices items according to offset/limit and wraps the result. It always
// returns a non-nil Items slice so the frontend never has to null-check.
func paginate[T any](items []T, offset, limit int) Page[T] {
	total := len(items)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := items[offset:end]
	if page == nil {
		page = []T{}
	}
	return Page[T]{Items: page, Total: total, Offset: offset, Limit: limit}
}
