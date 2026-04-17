package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"helpdesk/internal/models"

	"github.com/lib/pq"
)

type pendingTicket struct {
	id            int64
	customerEmail string
	category      string
	language      string
	priority      string
	createdAt     time.Time
}

type routingAgent struct {
	id              int64
	categorySkills  []string
	languages       []string
	shiftStartMin   int
	shiftEndMin     int
	maxCapacity     int
	currentLoad     int
	withinShift     bool
	languageMatched bool
}

type pendingAssignmentEngine struct {
	agents []*routingAgent
}

func newPendingAssignmentEngine(agents []*routingAgent) *pendingAssignmentEngine {
	return &pendingAssignmentEngine{agents: agents}
}

// selectAgent picks the best available agent for the ticket requirements.
// Selection prefers language matches when available, then chooses least-loaded agent (ID tie-breaker).
func (e *pendingAssignmentEngine) selectAgent(category, language string) (*routingAgent, bool) {
	var langMatched []*routingAgent
	var skillMatched []*routingAgent

	for _, agent := range e.agents {
		if !agent.withinShift || agent.currentLoad >= agent.maxCapacity {
			continue
		}
		if !contains(agent.categorySkills, category) {
			continue
		}

		agent.languageMatched = contains(agent.languages, language)
		skillMatched = append(skillMatched, agent)
		if agent.languageMatched {
			langMatched = append(langMatched, agent)
		}
	}

	selected := skillMatched
	if len(langMatched) > 0 {
		selected = langMatched
	}
	if len(selected) == 0 {
		return nil, false
	}

	sort.Slice(selected, func(i, j int) bool {
		if selected[i].currentLoad != selected[j].currentLoad {
			return selected[i].currentLoad < selected[j].currentLoad
		}
		return selected[i].id < selected[j].id
	})

	return selected[0], true
}

// processPending finds all unassigned and reopened tickets and attempts to assign them to available agents.
// Tickets are processed in priority order (urgent first) and then by creation time.
func (a *App) processPending() error {
	now := time.Now().UTC()

	pendingTickets, err := a.fetchPendingTickets()
	if err != nil {
		return err
	}
	if len(pendingTickets) == 0 {
		return nil
	}

	agents, err := a.fetchRoutingAgents(now)
	if err != nil {
		return err
	}

	engine := newPendingAssignmentEngine(agents)
	for _, ticket := range pendingTickets {
		agent, ok := engine.selectAgent(ticket.category, ticket.language)
		if !ok {
			a.addEvent(ticket.id, nil, "pending", "no eligible agent available right now; ticket kept in unassigned queue")
			continue
		}

		loadBefore := agent.currentLoad
		agentID := agent.id
		assigned, _ := a.applyAssignmentWithContext(ticket.id, agentID, ticket.language, agent.languageMatched, loadBefore, agent.maxCapacity, now)
		if assigned {
			agent.currentLoad++
		}
	}

	return nil
}

// fetchPendingTickets returns all tickets waiting for assignment, ordered by priority and FIFO within priority.
func (a *App) fetchPendingTickets() ([]pendingTicket, error) {
	rows, err := a.db.Query(`
		SELECT id, customer_email, category, language_preference, priority, created_at
		FROM tickets
		WHERE status IN ('unassigned', 'reopened')
		ORDER BY
			CASE priority
				WHEN 'urgent' THEN 1
				WHEN 'high' THEN 2
				WHEN 'medium' THEN 3
				WHEN 'low' THEN 4
			END,
			created_at ASC,
			id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tickets := make([]pendingTicket, 0)
	for rows.Next() {
		var ticket pendingTicket

		err := rows.Scan(&ticket.id, &ticket.customerEmail, &ticket.category, &ticket.language, &ticket.priority, &ticket.createdAt)
		if err != nil {
			return nil, err
		}
		tickets = append(tickets, ticket)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return tickets, nil
}

// fetchRoutingAgents loads online agents with spare capacity and computes whether they are currently within shift.
func (a *App) fetchRoutingAgents(now time.Time) ([]*routingAgent, error) {
	rows, err := a.db.Query(`
		SELECT a.id, a.skills, a.languages, a.shift_start_minutes, a.shift_end_minutes,
		       a.max_capacity, COUNT(t.id) as current_load
		FROM agents a
		LEFT JOIN tickets t ON a.id = t.current_agent_id AND t.status = 'assigned'
		WHERE a.is_online = true
		GROUP BY a.id, a.skills, a.languages, a.shift_start_minutes, a.shift_end_minutes, a.max_capacity
		HAVING COUNT(t.id) < a.max_capacity
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	agents := make([]*routingAgent, 0)
	for rows.Next() {
		agent := &routingAgent{}
		err := rows.Scan(&agent.id, pq.Array(&agent.categorySkills), pq.Array(&agent.languages),
			&agent.shiftStartMin, &agent.shiftEndMin, &agent.maxCapacity, &agent.currentLoad)
		if err != nil {
			return nil, err
		}

		agent.withinShift = withinShift(agent.shiftStartMin, agent.shiftEndMin, now)
		if !agent.withinShift {
			continue
		}

		agents = append(agents, agent)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return agents, nil
}

// assignTicketWithPreference attempts to assign a reopened ticket to its previous agent first.
// If the previous agent cannot take it (offline, out of shift, over capacity), falls back to normal routing.
func (a *App) assignTicketWithPreference(ticketID int64, category, language string, preferredAgentID *int64, now time.Time) (bool, *int64) {
	if preferredAgentID != nil && a.canTake(*preferredAgentID, category) {
		ok, id := a.applyPreferredAssignment(ticketID, *preferredAgentID, language, now)
		if ok {
			return ok, id
		}
	}
	return a.assignTicket(ticketID, "", category, language, "", now)
}

// applyPreferredAssignment assigns a reopened ticket back to its previous agent and records it as a preferred assignment.
func (a *App) applyPreferredAssignment(ticketID, agentID int64, language string, now time.Time) (bool, *int64) {
	var languages []string
	var currentLoad, maxCapacity int

	err := a.db.QueryRow(`
		SELECT languages, max_capacity,
		       (SELECT COUNT(*) FROM tickets WHERE current_agent_id = agents.id AND status = 'assigned') as current_load
		FROM agents WHERE id = $1
	`, agentID).Scan(pq.Array(&languages), &maxCapacity, &currentLoad)
	if err != nil {
		return false, nil
	}

	languageMatch := contains(languages, language)

	_, err = a.db.Exec(`
		UPDATE tickets SET current_agent_id = $1, status = 'assigned', assigned_at = $2, updated_at = $3 WHERE id = $4
	`, agentID, now, now, ticketID)
	if err != nil {
		return false, nil
	}

	_, err = a.db.Exec(`UPDATE agents SET updated_at = $1 WHERE id = $2`, now, agentID)
	if err != nil {
		a.logger.Printf("error updating agent updated_at: %v", err)
	}

	reason := assignmentReason(languageMatch, currentLoad, maxCapacity, true)
	a.addEvent(ticketID, &agentID, "assigned", reason)

	return true, &agentID
}

// assignTicket attempts to assign a ticket to the best available agent based on skills, language preference, and capacity.
// Returns true and the assigned agent ID if successful, false and nil if no agent is available.
func (a *App) assignTicket(ticketID int64, customerEmail, category, language, priority string, now time.Time) (bool, *int64) {
	candidates, err := a.getOrderedCandidates(category, language)
	if err != nil {
		a.logger.Printf("error getting candidates for ticket %d: %v", ticketID, err)
		return false, nil
	}

	for _, candidate := range candidates {
		if a.canTake(candidate.ID, category) {
			return a.applyAssignment(ticketID, candidate.ID, category, language, now)
		}
	}

	a.addEvent(ticketID, nil, "pending", fmt.Sprintf("no eligible agent available right now; ticket kept in unassigned queue"))
	return false, nil
}

// getOrderedCandidates finds all available agents who can handle the given category and language.
// Agents are ordered by language match preference, then by current load (least loaded first).
func (a *App) getOrderedCandidates(category, language string) ([]models.AgentSummary, error) {
	now := time.Now().UTC()

	rows, err := a.db.Query(`
		SELECT a.id, a.name, a.email, a.skills, a.languages, a.shift_start_minutes, a.shift_end_minutes,
			   a.max_capacity, COUNT(t.id) as current_load
		FROM agents a
		LEFT JOIN tickets t ON a.id = t.current_agent_id AND t.status = 'assigned'
		WHERE a.is_online = true
		GROUP BY a.id, a.name, a.email, a.skills, a.languages, a.shift_start_minutes, a.shift_end_minutes, a.max_capacity
		HAVING COUNT(t.id) < a.max_capacity
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var allCandidates []models.AgentSummary
	var langMatched []models.AgentSummary

	for rows.Next() {
		var candidate models.AgentSummary
		var skills, languages []string
		var shiftStartMin, shiftEndMin int
		var currentLoad int

		err := rows.Scan(&candidate.ID, &candidate.Name, &candidate.Email, pq.Array(&skills),
			pq.Array(&languages), &shiftStartMin, &shiftEndMin, &candidate.MaxCapacity,
			&currentLoad)
		if err != nil {
			return nil, err
		}

		if !contains(skills, category) {
			continue
		}

		candidate.WithinShift = withinShift(shiftStartMin, shiftEndMin, now)
		if !candidate.WithinShift {
			continue
		}

		candidate.CurrentOpen = currentLoad
		candidate.ShiftStartMinutes = shiftStartMin
		candidate.ShiftEndMinutes = shiftEndMin

		allCandidates = append(allCandidates, candidate)
		if contains(languages, language) {
			langMatched = append(langMatched, candidate)
		}
	}

	selected := allCandidates
	if len(langMatched) > 0 {
		selected = langMatched
	}

	sort.Slice(selected, func(i, j int) bool {
		if selected[i].CurrentOpen != selected[j].CurrentOpen {
			return selected[i].CurrentOpen < selected[j].CurrentOpen
		}
		return selected[i].ID < selected[j].ID
	})

	return selected, nil
}

// canTake determines if an agent can accept a ticket assignment.
// Checks agent availability (online status, shift time), skill compatibility,
// and current workload capacity.
// Parameters:
//   - agentID: the agent's unique identifier
//   - category: the ticket's skill category requirement
//
// Returns true if the agent can take the ticket, false otherwise.
func (a *App) canTake(agentID int64, category string) bool {
	var isOnline bool
	var shiftStartMin, shiftEndMin int
	var skills []string

	err := a.db.QueryRow(`
		SELECT is_online, shift_start_minutes, shift_end_minutes, skills
		FROM agents WHERE id = $1
	`, agentID).Scan(&isOnline, &shiftStartMin, &shiftEndMin, pq.Array(&skills))
	if err != nil {
		return false
	}

	if !isOnline || !withinShift(shiftStartMin, shiftEndMin, time.Now().UTC()) {
		return false
	}
	if !contains(skills, category) {
		return false
	}

	var currentLoad int
	err = a.db.QueryRow(`
		SELECT COUNT(*) FROM tickets WHERE current_agent_id = $1 AND status = 'assigned'
	`, agentID).Scan(&currentLoad)
	if err != nil {
		return false
	}

	var maxCapacity int
	err = a.db.QueryRow(`SELECT max_capacity FROM agents WHERE id = $1`, agentID).Scan(&maxCapacity)
	if err != nil {
		return false
	}

	return currentLoad < maxCapacity
}

// applyAssignment assigns a ticket to an agent and records the assignment.
// Updates the ticket status, agent's last assignment time, and creates an audit event.
// Parameters:
//   - ticketID: the ticket to assign
//   - agentID: the agent receiving the assignment
//   - category: ticket's skill category (for logging)
//   - language: ticket's language preference
//   - now: timestamp for the assignment
//
// Returns:
//   - bool: true if assignment succeeded
//   - *int64: pointer to assigned agent ID, or nil on failure
func (a *App) applyAssignment(ticketID, agentID int64, category, language string, now time.Time) (bool, *int64) {
	var agentName, agentEmail string
	var languages []string
	var currentLoad, maxCapacity int

	err := a.db.QueryRow(`
		SELECT name, email, languages, max_capacity,
			   (SELECT COUNT(*) FROM tickets WHERE current_agent_id = agents.id AND status = 'assigned') as current_load
		FROM agents WHERE id = $1
	`, agentID).Scan(&agentName, &agentEmail, pq.Array(&languages), &maxCapacity, &currentLoad)
	if err != nil {
		return false, nil
	}

	languageMatch := contains(languages, language)

	_, err = a.db.Exec(`
		UPDATE tickets SET current_agent_id = $1, status = 'assigned', assigned_at = $2, updated_at = $3 WHERE id = $4
	`, agentID, now, now, ticketID)
	if err != nil {
		return false, nil
	}

	_, err = a.db.Exec(`UPDATE agents SET updated_at = $1 WHERE id = $2`, now, agentID)
	if err != nil {
		a.logger.Printf("error updating agent updated_at: %v", err)
	}

	reason := assignmentReason(languageMatch, currentLoad, maxCapacity, false)
	a.addEvent(ticketID, &agentID, "assigned", reason)

	return true, &agentID
}

// applyAssignmentWithContext writes assignment updates using precomputed context values
// from the queue engine to avoid redundant read queries for load/language checks.
func (a *App) applyAssignmentWithContext(ticketID, agentID int64, language string, languageMatch bool, currentLoad, maxCapacity int, now time.Time) (bool, *int64) {
	_, err := a.db.Exec(`
		UPDATE tickets SET current_agent_id = $1, status = 'assigned', assigned_at = $2, updated_at = $3 WHERE id = $4
	`, agentID, now, now, ticketID)
	if err != nil {
		return false, nil
	}

	_, err = a.db.Exec(`UPDATE agents SET updated_at = $1 WHERE id = $2`, now, agentID)
	if err != nil {
		a.logger.Printf("error updating agent updated_at: %v", err)
	}

	reason := assignmentReason(languageMatch, currentLoad, maxCapacity, false)
	a.addEvent(ticketID, &agentID, "assigned", reason)

	return true, &agentID
}

// addEvent records an assignment event in the audit log.
// Creates a record of ticket state changes for tracking and debugging.
// Parameters:
//   - ticketID: the ticket involved in the event
//   - agentID: the agent involved (may be nil for system events)
//   - eventType: type of event ("assigned", "unassigned", etc.)
//   - reason: human-readable explanation of the event
func (a *App) addEvent(ticketID int64, agentID *int64, eventType, reason string) {
	_, err := a.db.Exec(
		`INSERT INTO assignments (ticket_id, agent_id, event_type, reason, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		ticketID, agentID, eventType, reason, time.Now().UTC(),
	)
	if err != nil {
		a.logger.Printf("error adding event: %v", err)
	}
}

// assignmentReason generates a human-readable explanation for why a ticket was assigned to an agent.
// Combines multiple factors into a semicolon-separated string for audit logging.
// Parameters:
//   - languageMatch: whether the agent's languages matched the ticket's language preference
//   - currentLoadBefore: agent's ticket count before assignment
//   - maxCapacity: agent's maximum concurrent ticket capacity
//   - preferred: whether this was a preferred agent assignment (e.g., for reopened tickets)
//
// Returns a descriptive string explaining the assignment decision.
func assignmentReason(languageMatch bool, currentLoadBefore, maxCapacity int, preferred bool) string {
	parts := []string{}
	if preferred {
		parts = append(parts, "reopened ticket returned to previous agent to preserve context")
	} else {
		parts = append(parts, "agent had the required skill")
	}
	if languageMatch {
		parts = append(parts, "preferred language matched")
	} else {
		parts = append(parts, "no language-matched agent was available, so system used skill-only fallback")
	}
	parts = append(parts, fmt.Sprintf("agent load before assignment was %d/%d", currentLoadBefore, maxCapacity))
	return strings.Join(parts, "; ")
}
