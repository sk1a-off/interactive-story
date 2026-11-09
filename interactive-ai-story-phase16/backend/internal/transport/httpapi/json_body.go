package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

const defaultJSONBodyLimit int64 = 1 << 20

type jsonBodyOptions struct {
	limit                 int64
	disallowUnknownFields bool
}

func decodeJSONRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeJSONRequestWithOptions(w, r, dst, jsonBodyOptions{limit: defaultJSONBodyLimit})
}

func decodeStrictJSONRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeJSONRequestWithOptions(w, r, dst, jsonBodyOptions{limit: defaultJSONBodyLimit, disallowUnknownFields: true})
}

func decodeJSONRequestWithLimit(w http.ResponseWriter, r *http.Request, dst any, limit int64) bool {
	return decodeJSONRequestWithOptions(w, r, dst, jsonBodyOptions{limit: limit})
}

func decodeJSONRequestWithOptions(w http.ResponseWriter, r *http.Request, dst any, options jsonBodyOptions) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
		return false
	}
	if options.limit <= 0 {
		options.limit = defaultJSONBodyLimit
	}

	r.Body = http.MaxBytesReader(w, r.Body, options.limit)
	decoder := json.NewDecoder(r.Body)
	if options.disallowUnknownFields {
		decoder.DisallowUnknownFields()
	}
	if err = decoder.Decode(dst); err != nil {
		writeJSONDecodeError(w, err)
		return false
	}
	if err = decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		writeJSONDecodeError(w, err)
		return false
	}
	return true
}

func writeJSONDecodeError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "JSON request body is too large.")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_json", "Request body must contain one valid JSON object.")
}
