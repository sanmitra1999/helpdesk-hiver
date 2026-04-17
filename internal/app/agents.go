package app

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"helpdesk/internal/models"

	"github.com/lib/pq"
)

// CreateAgent validates request data and stores a new agent in the database.
// It normalizes input data, checks for duplicates, and processes pending tickets if the agent goes online.
func (a *App) CreateAgent(in CreateAgentInput) (models.Agent, error) {
	name := strings.TrimSpace(in.Name)
	email := strings.ToLower(strings.TrimSpace(in.Email))
	skills := normalizeList(in.Skills)
	languages := normalizeList(in.Languages)

	if name == "" || email == "" {
		return models.Agent{}, &statusError{Code: http.StatusBadRequest, Message: "name and email are required"}
	}
	if len(skills) == 0 {
		return models.Agent{}, &statusError{Code: http.StatusBadRequest, Message: "at least one skill is required"}
	}
	if len(languages) == 0 {
		return models.Agent{}, &statusError{Code: http.StatusBadRequest, Message: "at least one language is required"}
	}
	if in.MaxCapacity <= 0 {
		return models.Agent{}, &statusError{Code: http.StatusBadRequest, Message: "max_capacity must be greater than 0"}
	}

	startMinutes, err := parseHHMM(in.ShiftStartUTC)
	if err != nil {
		return models.Agent{}, &statusError{Code: http.StatusBadRequest, Message: "shift_start_utc must be HH:MM in UTC"}
	}
	endMinutes, err := parseHHMM(in.ShiftEndUTC)
	if err != nil {
		return models.Agent{}, &statusError{Code: http.StatusBadRequest, Message: "shift_end_utc must be HH:MM in UTC"}
	}

	var exists bool
	err = a.db.QueryRow("SELECT EXISTS(SELECT 1 FROM agents WHERE email = $1)", email).Scan(&exists)
	if err != nil {
		return models.Agent{}, &statusError{Code: http.StatusInternalServerError, Message: err.Error()}
	}
	if exists {
		return models.Agent{}, &statusError{Code: http.StatusConflict, Message: "agent email already exists"}
	}

	now := time.Now().UTC()
	var agentID int64

	err = a.db.QueryRow(
		`INSERT INTO agents 
			(name, email, skills, languages, shift_start_utc, shift_end_utc, 
			 shift_start_minutes, shift_end_minutes, max_capacity, is_online, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 RETURNING id`,
		name, email, pq.Array(skills), pq.Array(languages),
		strings.TrimSpace(in.ShiftStartUTC), strings.TrimSpace(in.ShiftEndUTC),
		startMinutes, endMinutes, in.MaxCapacity, in.IsOnline, now, now,
	).Scan(&agentID)
	if err != nil {
		return models.Agent{}, &statusError{Code: http.StatusInternalServerError, Message: err.Error()}
	}

	if in.IsOnline {
		if err := a.processPending(); err != nil {
			a.logger.Printf("error processing pending tickets: %v", err)
		}
	}

	return models.Agent{
		ID: agentID, Name: name, Email: email, Skills: skills, Languages: languages,
		ShiftStart: strings.TrimSpace(in.ShiftStartUTC), ShiftEnd: strings.TrimSpace(in.ShiftEndUTC),
		ShiftStartMinutes: startMinutes, ShiftEndMinutes: endMinutes,
		MaxCapacity: in.MaxCapacity, IsOnline: in.IsOnline,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

// UpdateAgentStatus updates an agent's online state and requeues work if they go offline.
func (a *App) UpdateAgentStatus(agentID int64, online bool) (AgentStatusResponse, error) {
	now := time.Now().UTC()

	_, err := a.db.Exec(
		`UPDATE agents SET is_online = $1, updated_at = $2 WHERE id = $3`,
		online, now, agentID,
	)
	if err != nil {
		return AgentStatusResponse{}, err
	}

	resp := AgentStatusResponse{AgentID: agentID, IsOnline: online}

	if !online {
		rows, err := a.db.Query(
			`UPDATE tickets
			 SET current_agent_id = NULL, assigned_at = NULL, status = 'unassigned', updated_at = $1
			 WHERE status = 'assigned' AND current_agent_id = $2
			 RETURNING id`,
			now, agentID,
		)
		if err != nil {
			return AgentStatusResponse{}, err
		}
		defer rows.Close()

		for rows.Next() {
			var ticketID int64
			if err := rows.Scan(&ticketID); err != nil {
				return AgentStatusResponse{}, err
			}
			resp.RequeuedTickets++
			a.addEvent(ticketID, &agentID, "unassigned", "agent went offline, ticket returned to queue for reassignment")
		}
		if err := rows.Err(); err != nil {
			return AgentStatusResponse{}, err
		}
	}

	if online {
		if err := a.processPending(); err != nil {
			a.logger.Printf("error processing pending tickets: %v", err)
		}
	}

	return resp, nil
}

// GetAgentTickets returns the currently assigned tickets for a given agent.
func (a *App) GetAgentTickets(agentID int64) ([]models.Ticket, error) {
	var exists bool
	err := a.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM agents WHERE id = $1)`, agentID).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, &statusError{Code: http.StatusNotFound, Message: "agent not found"}
	}

	rows, err := a.db.Query(`
		SELECT t.id, t.customer_name, t.customer_email, t.category, t.language_preference, 
			   t.priority, t.status, t.created_at, t.updated_at, t.assigned_at,
			   a.name as agent_name, a.email as agent_email
		FROM tickets t
		LEFT JOIN agents a ON t.current_agent_id = a.id
		WHERE t.status = 'assigned' AND t.current_agent_id = $1
		ORDER BY 
			CASE t.priority 
				WHEN 'urgent' THEN 1 
				WHEN 'high' THEN 2 
				WHEN 'medium' THEN 3 
				WHEN 'low' THEN 4 
			END,
			t.created_at ASC
	`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tickets []models.Ticket
	for rows.Next() {
		var t models.Ticket
		var agentName, agentEmail sql.NullString
		var assignedAt sql.NullTime

		err := rows.Scan(&t.ID, &t.CustomerName, &t.CustomerEmail, &t.Category,
			&t.LanguagePreference, &t.Priority, &t.Status, &t.CreatedAt, &t.UpdatedAt,
			&assignedAt, &agentName, &agentEmail)
		if err != nil {
			return nil, err
		}

		if assignedAt.Valid {
			t.AssignedAt = &assignedAt.Time
		}
		if agentName.Valid {
			t.CurrentAgentName = &agentName.String
			t.CurrentAgentEmail = &agentEmail.String
		}

		tickets = append(tickets, t)
	}

	return tickets, nil
}
