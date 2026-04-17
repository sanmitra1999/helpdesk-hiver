package models

import "time"

// Agent represents a support agent who can handle tickets.
// Agents have skills, language capabilities, work shifts, and capacity limits.
type Agent struct {
	ID                int64     `json:"id"`              // Unique identifier for the agent
	Name              string    `json:"name"`            // Agent's full name
	Email             string    `json:"email"`           // Agent's email address (unique)
	Skills            []string  `json:"skills"`          // List of skills the agent possesses
	Languages         []string  `json:"languages"`       // Languages the agent can communicate in
	ShiftStart        string    `json:"shift_start_utc"` // Start time of agent's shift in HH:MM format (UTC)
	ShiftEnd          string    `json:"shift_end_utc"`   // End time of agent's shift in HH:MM format (UTC)
	ShiftStartMinutes int       `json:"-"`               // Shift start time converted to minutes since midnight (internal use)
	ShiftEndMinutes   int       `json:"-"`               // Shift end time converted to minutes since midnight (internal use)
	MaxCapacity       int       `json:"max_capacity"`    // Maximum number of tickets agent can handle simultaneously
	IsOnline          bool      `json:"is_online"`       // Whether the agent is currently available for assignments
	CreatedAt         time.Time `json:"created_at"`      // Timestamp when agent was created
	UpdatedAt         time.Time `json:"updated_at"`      // Timestamp when agent was last updated
}

// Ticket represents a customer support ticket that needs to be assigned to an agent.
// Tickets have priority levels and track their assignment and resolution status.
type Ticket struct {
	ID                 int64      `json:"id"`                            // Unique identifier for the ticket
	CustomerName       string     `json:"customer_name"`                 // Name of the customer who created the ticket
	CustomerEmail      string     `json:"customer_email"`                // Email address of the customer
	Category           string     `json:"category"`                      // Category of the issue (e.g., "technical", "billing")
	LanguagePreference string     `json:"language_preference"`           // Preferred language for communication
	Priority           string     `json:"priority"`                      // Priority level: "low", "medium", "high", "urgent"
	Status             string     `json:"status"`                        // Current status: "unassigned", "assigned", "resolved", "reopened"
	CurrentAgentID     *int64     `json:"current_agent_id,omitempty"`    // ID of currently assigned agent (null if unassigned)
	CurrentAgentName   *string    `json:"current_agent_name,omitempty"`  // Name of currently assigned agent (for API responses)
	CurrentAgentEmail  *string    `json:"current_agent_email,omitempty"` // Email of currently assigned agent (for API responses)
	CreatedAt          time.Time  `json:"created_at"`                    // Timestamp when ticket was created
	AssignedAt         *time.Time `json:"assigned_at,omitempty"`         // Timestamp when ticket was assigned to an agent
	ResolvedAt         *time.Time `json:"resolved_at,omitempty"`         // Timestamp when ticket was resolved
	UpdatedAt          time.Time  `json:"updated_at"`                    // Timestamp when ticket was last updated
}

// AssignmentEvent represents an event in the lifecycle of a ticket assignment.
// Events track when tickets are assigned, unassigned, resolved, etc., with human-readable reasons.
type AssignmentEvent struct {
	ID         int64     `json:"id"`                    // Unique identifier for the event
	TicketID   int64     `json:"ticket_id"`             // ID of the ticket this event relates to
	AgentID    *int64    `json:"agent_id,omitempty"`    // ID of the agent involved (null for system events)
	EventType  string    `json:"event_type"`            // Type of event: "assigned", "unassigned", "resolved", "reopened", "pending"
	Reason     string    `json:"reason"`                // Human-readable explanation of why the event occurred
	CreatedAt  time.Time `json:"created_at"`            // Timestamp when the event occurred
	AgentName  *string   `json:"agent_name,omitempty"`  // Name of the agent involved (for API responses)
	AgentEmail *string   `json:"agent_email,omitempty"` // Email of the agent involved (for API responses)
}

// AgentSummary provides a snapshot of an agent's current workload and availability.
// Used for displaying agent status in dashboards and assignment algorithms.
type AgentSummary struct {
	ID                int64  `json:"id"`                   // Unique identifier for the agent
	Name              string `json:"name"`                 // Agent's full name
	Email             string `json:"email"`                // Agent's email address
	IsOnline          bool   `json:"is_online"`            // Whether the agent is currently online
	WithinShift       bool   `json:"within_shift"`         // Whether current time is within agent's shift
	ShiftStartMinutes int    `json:"-"`                    // Shift start time in minutes (internal use)
	ShiftEndMinutes   int    `json:"-"`                    // Shift end time in minutes (internal use)
	CurrentOpen       int    `json:"current_open_tickets"` // Number of tickets currently assigned to this agent
	MaxCapacity       int    `json:"max_capacity"`         // Maximum number of tickets agent can handle
	RemainingCap      int    `json:"remaining_capacity"`   // Remaining capacity (max_capacity - current_open)
}

// TicketDetail provides comprehensive information about a ticket including its assignment history.
// Used for detailed ticket views and audit trails.
type TicketDetail struct {
	Ticket       Ticket            `json:"ticket"`                  // The ticket information
	CurrentAgent *AgentSummary     `json:"current_agent,omitempty"` // Summary of currently assigned agent
	Events       []AssignmentEvent `json:"events"`                  // Chronological list of assignment events
}
