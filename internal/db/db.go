package db

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

// OpenDB opens a PostgreSQL database connection.
func OpenDB(host, user, password, dbname string, port int) (*sql.DB, error) {
	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)
	db, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}

// InitSchema creates the database schema for the helpdesk application.
// Creates three main tables:
//   - agents: stores agent information, skills, shifts, and capacity
//   - tickets: stores customer ticket data and assignment status
//   - assignments: audit log of ticket assignment events
//
// Also creates indexes for efficient querying of ticket status and assignments.
// Uses IF NOT EXISTS to avoid errors on re-initialization.
func InitSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS agents (
		id BIGSERIAL PRIMARY KEY,
		name TEXT NOT NULL,
		email TEXT UNIQUE NOT NULL,
		skills TEXT[] NOT NULL,
		languages TEXT[] NOT NULL,
		shift_start_utc TEXT NOT NULL,
		shift_end_utc TEXT NOT NULL,
		shift_start_minutes INT NOT NULL,
		shift_end_minutes INT NOT NULL,
		max_capacity INT NOT NULL,
		is_online BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMP WITH TIME ZONE NOT NULL,
		updated_at TIMESTAMP WITH TIME ZONE NOT NULL
	);

	CREATE TABLE IF NOT EXISTS tickets (
		id BIGSERIAL PRIMARY KEY,
		customer_name TEXT NOT NULL,
		customer_email TEXT NOT NULL,
		category TEXT NOT NULL,
		language_preference TEXT NOT NULL,
		priority TEXT NOT NULL,
		status TEXT NOT NULL,
		current_agent_id BIGINT,
		created_at TIMESTAMP WITH TIME ZONE NOT NULL,
		assigned_at TIMESTAMP WITH TIME ZONE,
		resolved_at TIMESTAMP WITH TIME ZONE,
		updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
		FOREIGN KEY (current_agent_id) REFERENCES agents(id)
	);

	CREATE TABLE IF NOT EXISTS assignments (
		id BIGSERIAL PRIMARY KEY,
		ticket_id BIGINT NOT NULL,
		agent_id BIGINT,
		event_type TEXT NOT NULL,
		reason TEXT NOT NULL,
		created_at TIMESTAMP WITH TIME ZONE NOT NULL,
		FOREIGN KEY (ticket_id) REFERENCES tickets(id),
		FOREIGN KEY (agent_id) REFERENCES agents(id)
	);

	CREATE INDEX IF NOT EXISTS idx_tickets_status ON tickets(status);
	CREATE INDEX IF NOT EXISTS idx_tickets_current_agent_id ON tickets(current_agent_id);
	CREATE INDEX IF NOT EXISTS idx_assignments_ticket_id ON assignments(ticket_id);
	`

	_, err := db.Exec(schema)
	return err
}
