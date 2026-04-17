package app

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"helpdesk/internal/models"
)

// CreateTicket validates and stores a new ticket, then tries immediate assignment.
func (a *App) CreateTicket(in CreateTicketInput) (models.TicketDetail, error) {
	customerName := strings.TrimSpace(in.CustomerName)
	customerEmail := strings.ToLower(strings.TrimSpace(in.CustomerEmail))
	category := normalizeValue(in.Category)
	language := normalizeValue(in.LanguagePreference)
	priority := normalizeValue(in.Priority)
	if customerName == "" || customerEmail == "" || category == "" || language == "" || priority == "" {
		return models.TicketDetail{}, &statusError{Code: http.StatusBadRequest, Message: "customer_name, customer_email, category, language_preference and priority are required"}
	}
	if !isValidPriority(priority) {
		return models.TicketDetail{}, &statusError{Code: http.StatusBadRequest, Message: "priority must be one of low, medium, high, urgent"}
	}

	now := time.Now().UTC()
	var ticketID int64

	err := a.db.QueryRow(
		`INSERT INTO tickets
			(customer_name, customer_email, category, language_preference, priority,
			 status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, 'unassigned', $6, $7)
		 RETURNING id`,
		customerName, customerEmail, category, language, priority, now, now,
	).Scan(&ticketID)
	if err != nil {
		return models.TicketDetail{}, err
	}

	assigned, agentID := a.assignTicket(ticketID, customerEmail, category, language, priority, now)
	var status string
	var assignedAt *time.Time
	var currentAgentID *int64

	if assigned && agentID != nil {
		status = "assigned"
		assignedAt = &now
		currentAgentID = agentID
	} else {
		status = "unassigned"
	}

	_, err = a.db.Exec(
		`UPDATE tickets SET status = $1, current_agent_id = $2, assigned_at = $3, updated_at = $4 WHERE id = $5`,
		status, currentAgentID, assignedAt, now, ticketID,
	)
	if err != nil {
		return models.TicketDetail{}, err
	}

	return a.GetTicketDetail(ticketID)
}

// ResolveTicket resolves an assigned ticket and triggers reassignment for queued work.
func (a *App) ResolveTicket(ticketID int64) (ResolveTicketResponse, error) {
	now := time.Now().UTC()

	var status string
	var currentAgentID *int64
	err := a.db.QueryRow(
		`SELECT status, current_agent_id FROM tickets WHERE id = $1`,
		ticketID,
	).Scan(&status, &currentAgentID)
	if err != nil {
		if err == sql.ErrNoRows {
			return ResolveTicketResponse{}, &statusError{Code: http.StatusNotFound, Message: "ticket not found"}
		}
		return ResolveTicketResponse{}, err
	}

	if status != "assigned" {
		return ResolveTicketResponse{}, &statusError{Code: http.StatusConflict, Message: "only assigned tickets can be resolved"}
	}

	_, err = a.db.Exec(
		`UPDATE tickets SET status = 'resolved', resolved_at = $1, updated_at = $2 WHERE id = $3`,
		now, now, ticketID,
	)
	if err != nil {
		return ResolveTicketResponse{}, err
	}

	a.addEvent(ticketID, currentAgentID, "resolved", "ticket marked resolved")

	if err := a.processPending(); err != nil {
		a.logger.Printf("error processing pending tickets: %v", err)
	}

	return ResolveTicketResponse{TicketID: ticketID, Status: "resolved"}, nil
}

// ReopenTicket moves a resolved ticket back into routing and records the reopen event.
func (a *App) ReopenTicket(ticketID int64) (ReopenTicketResponse, error) {
	now := time.Now().UTC()

	var status, category, language string
	var currentAgentID *int64
	err := a.db.QueryRow(
		`SELECT status, current_agent_id, category, language_preference FROM tickets WHERE id = $1`,
		ticketID,
	).Scan(&status, &currentAgentID, &category, &language)
	if err != nil {
		if err == sql.ErrNoRows {
			return ReopenTicketResponse{}, &statusError{Code: http.StatusNotFound, Message: "ticket not found"}
		}
		return ReopenTicketResponse{}, err
	}

	if status != "resolved" {
		return ReopenTicketResponse{}, &statusError{Code: http.StatusConflict, Message: "only resolved tickets can be reopened"}
	}

	previousAgentID := currentAgentID

	_, err = a.db.Exec(
		`UPDATE tickets SET status = 'reopened', resolved_at = NULL, current_agent_id = NULL, assigned_at = NULL, updated_at = $1 WHERE id = $2`,
		now, ticketID,
	)
	if err != nil {
		return ReopenTicketResponse{}, err
	}

	a.addEvent(ticketID, previousAgentID, "reopened", "ticket reopened; system will try the previous agent first and then fallback to normal routing")
	a.assignTicketWithPreference(ticketID, category, language, previousAgentID, now)

	return ReopenTicketResponse{TicketID: ticketID, Status: "reopened"}, nil
}

// GetTicketDetail loads the ticket, current assignment summary, and audit events.
func (a *App) GetTicketDetail(ticketID int64) (models.TicketDetail, error) {
	var t models.Ticket
	var resolvedAt sql.NullTime
	var currentAgentID sql.NullInt64
	var assignedAt sql.NullTime

	err := a.db.QueryRow(`
		SELECT t.id, t.customer_name, t.customer_email, t.category, t.language_preference,
			   t.priority, t.status, t.created_at, t.updated_at, t.assigned_at, t.resolved_at,
			   t.current_agent_id, a.name as agent_name, a.email as agent_email
		FROM tickets t
		LEFT JOIN agents a ON t.current_agent_id = a.id
		WHERE t.id = $1
	`, ticketID).Scan(
		&t.ID, &t.CustomerName, &t.CustomerEmail, &t.Category, &t.LanguagePreference,
		&t.Priority, &t.Status, &t.CreatedAt, &t.UpdatedAt, &assignedAt, &resolvedAt,
		&currentAgentID, &t.CurrentAgentName, &t.CurrentAgentEmail,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return models.TicketDetail{}, &statusError{Code: http.StatusNotFound, Message: "ticket not found"}
		}
		return models.TicketDetail{}, err
	}

	if assignedAt.Valid {
		t.AssignedAt = &assignedAt.Time
	}
	if resolvedAt.Valid {
		t.ResolvedAt = &resolvedAt.Time
	}
	if currentAgentID.Valid {
		agentID := currentAgentID.Int64
		t.CurrentAgentID = &agentID
	}

	detail := models.TicketDetail{Ticket: t}

	if t.CurrentAgentID != nil {
		agentSummary, err := a.getAgentSummary(*t.CurrentAgentID)
		if err == nil {
			detail.CurrentAgent = &agentSummary
		}
	}

	events, err := a.getTicketEvents(ticketID)
	if err != nil {
		return models.TicketDetail{}, err
	}
	detail.Events = events

	return detail, nil
}

// GetAssignmentSummary returns the current workload snapshot for all agents.
func (a *App) GetAssignmentSummary() ([]models.AgentSummary, error) {
	rows, err := a.db.Query(`
		SELECT a.id, a.name, a.email, a.is_online, a.shift_start_minutes, a.shift_end_minutes,
			   a.max_capacity, COUNT(t.id) as current_open
		FROM agents a
		LEFT JOIN tickets t ON a.id = t.current_agent_id AND t.status = 'assigned'
		GROUP BY a.id, a.name, a.email, a.is_online, a.shift_start_minutes, a.shift_end_minutes, a.max_capacity
		ORDER BY current_open DESC, a.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []models.AgentSummary
	now := time.Now().UTC()

	for rows.Next() {
		var summary models.AgentSummary
		var currentOpen int

		err := rows.Scan(&summary.ID, &summary.Name, &summary.Email, &summary.IsOnline, &summary.ShiftStartMinutes,
			&summary.ShiftEndMinutes, &summary.MaxCapacity, &currentOpen)
		if err != nil {
			return nil, err
		}

		summary.CurrentOpen = currentOpen
		summary.WithinShift = withinShift(summary.ShiftStartMinutes, summary.ShiftEndMinutes, now)
		summary.RemainingCap = max(0, summary.MaxCapacity-currentOpen)

		summaries = append(summaries, summary)
	}

	return summaries, nil
}

func (a *App) getAgentSummary(agentID int64) (models.AgentSummary, error) {
	var summary models.AgentSummary
	var currentOpen int
	now := time.Now().UTC()

	err := a.db.QueryRow(`
		SELECT a.id, a.name, a.email, a.is_online, a.shift_start_minutes, a.shift_end_minutes,
			   a.max_capacity, COUNT(t.id) as current_open
		FROM agents a
		LEFT JOIN tickets t ON a.id = t.current_agent_id AND t.status = 'assigned'
		WHERE a.id = $1
		GROUP BY a.id, a.name, a.email, a.is_online, a.shift_start_minutes, a.shift_end_minutes, a.max_capacity
	`, agentID).Scan(&summary.ID, &summary.Name, &summary.Email, &summary.IsOnline, &summary.ShiftStartMinutes,
		&summary.ShiftEndMinutes, &summary.MaxCapacity, &currentOpen)
	if err != nil {
		return summary, err
	}

	summary.CurrentOpen = currentOpen
	summary.WithinShift = withinShift(summary.ShiftStartMinutes, summary.ShiftEndMinutes, now)
	summary.RemainingCap = max(0, summary.MaxCapacity-currentOpen)

	return summary, nil
}

func (a *App) getTicketEvents(ticketID int64) ([]models.AssignmentEvent, error) {
	rows, err := a.db.Query(`
		SELECT a.id, a.ticket_id, a.agent_id, a.event_type, a.reason, a.created_at,
			   ag.name as agent_name, ag.email as agent_email
		FROM assignments a
		LEFT JOIN agents ag ON a.agent_id = ag.id
		WHERE a.ticket_id = $1
		ORDER BY a.created_at ASC
	`, ticketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []models.AssignmentEvent
	for rows.Next() {
		var event models.AssignmentEvent
		var agentID sql.NullInt64
		var agentName, agentEmail sql.NullString

		err := rows.Scan(&event.ID, &event.TicketID, &agentID, &event.EventType, &event.Reason, &event.CreatedAt,
			&agentName, &agentEmail)
		if err != nil {
			return nil, err
		}

		if agentID.Valid {
			aid := agentID.Int64
			event.AgentID = &aid
		}
		if agentName.Valid {
			event.AgentName = &agentName.String
		}
		if agentEmail.Valid {
			event.AgentEmail = &agentEmail.String
		}

		events = append(events, event)
	}

	return events, nil
}
