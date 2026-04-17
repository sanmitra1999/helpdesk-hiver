package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

// Routes registers all HTTP endpoint handlers for the helpdesk API.
func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if err := a.db.Ping(); err != nil {
			writeError(w, http.StatusServiceUnavailable, "database unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/agents", a.handleAgents)
	mux.HandleFunc("/agents/", a.handleAgentsSubroutes)
	mux.HandleFunc("/tickets", a.handleTickets)
	mux.HandleFunc("/tickets/", a.handleTicketsSubroutes)
	mux.HandleFunc("/assignments/summary", a.handleAssignmentSummary)

	return mux
}

func (a *App) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/agents" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var in CreateAgentInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	agent, err := a.CreateAgent(in)
	if err != nil {
		a.handleError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, agent)
}

func (a *App) handleAgentsSubroutes(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(r.URL.Path)
	if len(parts) != 3 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid agent id")
		return
	}

	switch {
	case parts[2] == "status" && r.Method == http.MethodPatch:
		var in UpdateAgentStatusInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}

		resp, err := a.UpdateAgentStatus(id, in.Online)
		if err != nil {
			a.handleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	case parts[2] == "tickets" && r.Method == http.MethodGet:
		tickets, err := a.GetAgentTickets(id)
		if err != nil {
			a.handleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tickets": tickets})
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (a *App) handleTickets(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/tickets" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var in CreateTicketInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	detail, err := a.CreateTicket(in)
	if err != nil {
		a.handleError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, detail)
}

func (a *App) handleTicketsSubroutes(w http.ResponseWriter, r *http.Request) {
	parts := splitPath(r.URL.Path)
	if len(parts) < 2 || len(parts) > 3 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid ticket id")
		return
	}

	if len(parts) == 2 && r.Method == http.MethodGet {
		detail, err := a.GetTicketDetail(id)
		if err != nil {
			a.handleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
		return
	}

	if len(parts) != 3 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	switch {
	case parts[2] == "resolve" && r.Method == http.MethodPatch:
		resp, err := a.ResolveTicket(id)
		if err != nil {
			a.handleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	case parts[2] == "reopen" && r.Method == http.MethodPatch:
		resp, err := a.ReopenTicket(id)
		if err != nil {
			a.handleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (a *App) handleAssignmentSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	summary, err := a.GetAssignmentSummary()
	if err != nil {
		a.handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"agents": summary})
}

func (a *App) handleError(w http.ResponseWriter, err error) {
	var se *statusError
	if errors.As(err, &se) {
		writeError(w, se.Code, se.Message)
		return
	}

	a.logger.Printf("internal error: %v", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, map[string]string{"error": message})
}
