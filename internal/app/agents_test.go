package app

import (
	"io"
	"log"
	"net/http"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestCreateAgent_ValidationError(t *testing.T) {
	a := &App{logger: log.New(io.Discard, "", 0)}

	_, err := a.CreateAgent(CreateAgentInput{})
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}

	se, ok := err.(*statusError)
	if !ok {
		t.Fatalf("expected statusError, got %T", err)
	}
	if se.Code != http.StatusBadRequest {
		t.Fatalf("expected status code %d, got %d", http.StatusBadRequest, se.Code)
	}
}

func TestCreateAgent_OnlineTriggersPendingProcessing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	a := &App{db: db, logger: log.New(io.Discard, "", 0)}

	in := CreateAgentInput{
		Name:          "Asha",
		Email:         "Asha@Example.com",
		Skills:        []string{"billing", "billing"},
		Languages:     []string{"english"},
		ShiftStartUTC: "09:00",
		ShiftEndUTC:   "17:00",
		MaxCapacity:   2,
		IsOnline:      true,
	}

	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM agents WHERE email = \$1\)`).
		WithArgs("asha@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	mock.ExpectQuery(`INSERT INTO agents`).
		WithArgs(
			"Asha",
			"asha@example.com",
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			"09:00",
			"17:00",
			540,
			1020,
			2,
			true,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)))

	mock.ExpectQuery(`SELECT id, customer_email, category, language_preference, priority, created_at`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "customer_email", "category", "language_preference", "priority", "created_at"}))

	agent, err := a.CreateAgent(in)
	if err != nil {
		t.Fatalf("CreateAgent returned error: %v", err)
	}

	if agent.ID != 11 {
		t.Fatalf("expected id 11, got %d", agent.ID)
	}
	if agent.Email != "asha@example.com" {
		t.Fatalf("expected normalized email, got %q", agent.Email)
	}
	if !agent.IsOnline {
		t.Fatalf("expected online agent")
	}
	if len(agent.Skills) != 1 || agent.Skills[0] != "billing" {
		t.Fatalf("expected deduplicated normalized skills, got %#v", agent.Skills)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestUpdateAgentStatus_OfflineRequeuesAssignedTickets(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	a := &App{db: db, logger: log.New(io.Discard, "", 0)}

	mock.ExpectExec(`UPDATE agents SET is_online = \$1, updated_at = \$2 WHERE id = \$3`).
		WithArgs(false, sqlmock.AnyArg(), int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	requeueRows := sqlmock.NewRows([]string{"id"}).AddRow(int64(101)).AddRow(int64(102))
	mock.ExpectQuery(`UPDATE tickets`).
		WithArgs(sqlmock.AnyArg(), int64(7)).
		WillReturnRows(requeueRows)

	mock.ExpectExec(`INSERT INTO assignments`).
		WithArgs(int64(101), int64(7), "unassigned", "agent went offline, ticket returned to queue for reassignment", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectExec(`INSERT INTO assignments`).
		WithArgs(int64(102), int64(7), "unassigned", "agent went offline, ticket returned to queue for reassignment", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	resp, err := a.UpdateAgentStatus(7, false)
	if err != nil {
		t.Fatalf("UpdateAgentStatus returned error: %v", err)
	}
	if resp.AgentID != 7 {
		t.Fatalf("expected agent id 7, got %d", resp.AgentID)
	}
	if resp.IsOnline {
		t.Fatalf("expected offline status")
	}
	if resp.RequeuedTickets != 2 {
		t.Fatalf("expected 2 requeued tickets, got %d", resp.RequeuedTickets)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestUpdateAgentStatus_OnlineTriggersPendingProcessing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	a := &App{db: db, logger: log.New(io.Discard, "", 0)}

	mock.ExpectExec(`UPDATE agents SET is_online = \$1, updated_at = \$2 WHERE id = \$3`).
		WithArgs(true, sqlmock.AnyArg(), int64(9)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	pendingRows := sqlmock.NewRows([]string{"id", "customer_email", "category", "language_preference", "priority", "created_at"})
	mock.ExpectQuery(`SELECT id, customer_email, category, language_preference, priority, created_at`).
		WillReturnRows(pendingRows)

	resp, err := a.UpdateAgentStatus(9, true)
	if err != nil {
		t.Fatalf("UpdateAgentStatus returned error: %v", err)
	}
	if resp.AgentID != 9 {
		t.Fatalf("expected agent id 9, got %d", resp.AgentID)
	}
	if !resp.IsOnline {
		t.Fatalf("expected online status")
	}
	if resp.RequeuedTickets != 0 {
		t.Fatalf("expected 0 requeued tickets, got %d", resp.RequeuedTickets)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestGetAgentTickets_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	a := &App{db: db, logger: log.New(io.Discard, "", 0)}

	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM agents WHERE id = \$1\)`).
		WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	_, err = a.GetAgentTickets(99)
	if err == nil {
		t.Fatalf("expected error for missing agent")
	}

	se, ok := err.(*statusError)
	if !ok {
		t.Fatalf("expected statusError, got %T", err)
	}
	if se.Code != http.StatusNotFound {
		t.Fatalf("expected status code %d, got %d", http.StatusNotFound, se.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestGetAgentTickets_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	a := &App{db: db, logger: log.New(io.Discard, "", 0)}

	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM agents WHERE id = \$1\)`).
		WithArgs(int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	assignedAt := time.Now().UTC().Truncate(time.Second)
	createdAt := assignedAt.Add(-2 * time.Hour)
	updatedAt := assignedAt.Add(-1 * time.Hour)

	ticketRows := sqlmock.NewRows([]string{
		"id", "customer_name", "customer_email", "category", "language_preference",
		"priority", "status", "created_at", "updated_at", "assigned_at", "agent_name", "agent_email",
	}).AddRow(
		int64(301), "Ravi", "ravi@example.com", "billing", "english",
		"high", "assigned", createdAt, updatedAt, assignedAt, "Asha", "asha@example.com",
	)

	mock.ExpectQuery(`SELECT t.id, t.customer_name, t.customer_email, t.category, t.language_preference`).
		WithArgs(int64(5)).
		WillReturnRows(ticketRows)

	tickets, err := a.GetAgentTickets(5)
	if err != nil {
		t.Fatalf("GetAgentTickets returned error: %v", err)
	}
	if len(tickets) != 1 {
		t.Fatalf("expected 1 ticket, got %d", len(tickets))
	}
	if tickets[0].ID != 301 {
		t.Fatalf("expected ticket id 301, got %d", tickets[0].ID)
	}
	if tickets[0].CurrentAgentName == nil || *tickets[0].CurrentAgentName != "Asha" {
		t.Fatalf("expected current agent name Asha, got %#v", tickets[0].CurrentAgentName)
	}
	if tickets[0].AssignedAt == nil {
		t.Fatalf("expected assigned_at to be populated")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
