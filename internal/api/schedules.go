package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"post-gen/internal/models"
)

// handleSchedules serves the schedule collection at /schedules.
func (s server) handleSchedules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		schedules, err := s.engine.Schedules(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"schedules": schedules})

	case http.MethodPost:
		schedule, ok := decodeSchedule(w, r)
		if !ok {
			return
		}

		created, err := s.engine.CreateSchedule(r.Context(), schedule)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, created)

	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

// handleScheduleByID serves /schedules/{id} and /schedules/{id}/run.
func (s server) handleScheduleByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/schedules/")
	if rest == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "schedule id is required"})
		return
	}

	idPart, action, _ := strings.Cut(rest, "/")
	id, err := strconv.Atoi(idPart)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid schedule id"})
		return
	}

	if action == "run" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		jobID, err := s.engine.RunSchedule(r.Context(), id)
		if err != nil {
			// A refusal to start is usually "a job is already running" or "no
			// links available" - a conflict with current state, not a fault.
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job_id": jobID, "status": "started"})
		return
	}

	switch r.Method {
	case http.MethodPut:
		schedule, ok := decodeSchedule(w, r)
		if !ok {
			return
		}
		schedule.ID = id

		updated, err := s.engine.UpdateSchedule(r.Context(), schedule)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if updated == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "schedule not found"})
			return
		}
		writeJSON(w, http.StatusOK, updated)

	case http.MethodDelete:
		if err := s.engine.DeleteSchedule(r.Context(), id); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		methodNotAllowed(w, http.MethodPut+", "+http.MethodDelete)
	}
}

// decodeSchedule reads a schedule payload, reporting a 400 on malformed input.
func decodeSchedule(w http.ResponseWriter, r *http.Request) (models.JobSchedule, bool) {
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var schedule models.JobSchedule
	if err := decoder.Decode(&schedule); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return schedule, false
	}

	schedule.Name = strings.TrimSpace(schedule.Name)
	schedule.DailyAt = strings.TrimSpace(schedule.DailyAt)
	return schedule, true
}
