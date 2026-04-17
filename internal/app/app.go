package app

import (
	"database/sql"
	"log"
)

type App struct {
	db     *sql.DB     
	logger *log.Logger
}


type statusError struct {
	Code    int    
	Message string 
	Err     error
}


func (e *statusError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Message
}

// CreateAgentInput defines the payload required to register a new helpdesk agent.
type CreateAgentInput struct {
	Name          string   `json:"name"`            // Agent's full name
	Email         string   `json:"email"`           // Agent's email address (must be unique)
	Skills        []string `json:"skills"`          // List of skills the agent possesses
	Languages     []string `json:"languages"`       // Languages the agent can communicate in
	ShiftStartUTC string   `json:"shift_start_utc"` // Shift start time in "HH:MM" format (UTC)
	ShiftEndUTC   string   `json:"shift_end_utc"`   // Shift end time in "HH:MM" format (UTC)
	MaxCapacity   int      `json:"max_capacity"`    // Maximum concurrent tickets the agent can handle
	IsOnline      bool     `json:"is_online"`       // Whether agent should be available immediately
}

// UpdateAgentStatusInput defines the payload for updating an agent's online status.
type UpdateAgentStatusInput struct {
	Online bool `json:"online"` // Whether to set agent online (true) or offline (false)
}

// CreateTicketInput defines the payload required to create a new support ticket.
type CreateTicketInput struct {
	CustomerName       string `json:"customer_name"`       // Name of the customer creating the ticket
	CustomerEmail      string `json:"customer_email"`      // Email address of the customer
	Category           string `json:"category"`            // Category of the issue (e.g., "technical", "billing")
	LanguagePreference string `json:"language_preference"` // Preferred language for communication
	Priority           string `json:"priority"`            // Priority level: "low", "medium", "high", "urgent"
}

// AgentStatusResponse contains the result of updating an agent's online status.
type AgentStatusResponse struct {
	AgentID         int64 `json:"agent_id"`         // ID of the agent whose status was updated
	IsOnline        bool  `json:"is_online"`        // New online status of the agent
	RequeuedTickets int   `json:"requeued_tickets"` // Number of tickets requeued when agent went offline
}

// ResolveTicketResponse contains the result of resolving a ticket.
type ResolveTicketResponse struct {
	TicketID int64  `json:"ticket_id"` // ID of the resolved ticket
	Status   string `json:"status"`    // New status of the ticket ("resolved")
}

// ReopenTicketResponse contains the result of reopening a ticket.
type ReopenTicketResponse struct {
	TicketID int64  `json:"ticket_id"` // ID of the reopened ticket
	Status   string `json:"status"`    // New status of the ticket ("reopened")
}

// New creates the App instance with database connection and initializes pending tickets.
func New(db *sql.DB, logger *log.Logger) (*App, error) {
	a := &App{
		db:     db,
		logger: logger,
	}
	if err := a.processPending(); err != nil {
		return nil, err
	}
	return a, nil
}
