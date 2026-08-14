package api

import (
	"encoding/json"
	"net/http"

	"post-gen/internal/models"
)

// handleSettings serves the runtime configuration screen.
//
// GET returns effective values with the source of each. PUT stores overrides.
// Secrets are write-only by design: the response reports only whether one is
// configured, never the value.
func (s server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		view, err := s.engine.Settings(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, view)

	case http.MethodPut:
		defer r.Body.Close()
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()

		var update models.SettingsUpdate
		if err := decoder.Decode(&update); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
			return
		}

		if err := s.engine.SaveSettings(r.Context(), update); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		view, err := s.engine.Settings(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, view)

	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPut)
	}
}
