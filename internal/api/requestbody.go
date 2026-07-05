package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const maxJSONBodyBytes int64 = 1 << 20

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes))
	if err := decoder.Decode(dst); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			respondError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		respondError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			respondError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		if errors.Is(err, io.EOF) {
			return true
		}
		respondError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	respondError(w, http.StatusBadRequest, "invalid JSON")
	return false
}
