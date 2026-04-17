package app

import (
	"database/sql"
	"io"
	"log"
	"net/http"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestCreateTicket_ValidationErrorMissingFields(t *testing.T) {
	a := &App{logger: log.New(io.Discard, "", 0)}

	_, err := a.CreateTicket(CreateTicketInput{})
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}

	se, ok := err.(*statusError)
	if !ok {
		t.Fatalf("expected statusError, got %T", err)
	}
	if se.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, se.Code)
	}
}

func TestCreateTicket_InvalidPriority(t *testing.T) {
	a := &App{logger: log.New(io.Discard, "", 0)}

	_, err := a.CreateTicket(CreateTicketInput{
		CustomerName:       "Ravi",
		CustomerEmail:      "ravi@example.com",
		Category:           "billing",
		LanguagePreference: "english",
		Priority:           "critical",
	})
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}

	se, ok := err.(*statusError)
	if !ok {
		t.Fatalf("expected statusError, got %T", err)
	}
	if se.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, se.Code)
	}
}

func TestResolveTicket_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	a := &App{db: db, logger: log.New(io.Discard, "", 0)}

	mock.ExpectQuery(`SELECT status, current_agent_id FROM tickets WHERE id = \$1`).
		WithArgs(int64(100)).
		WillReturnError(sql.ErrNoRows)

	_, err = a.ResolveTicket(100)
	if err == nil {
		t.Fatalf("expected not found error")
	}

	se, ok := err.(*statusError)
	if !ok {
		t.Fatalf("expected statusError, got %T", err)
	}
	if se.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, se.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestResolveTicket_StatusConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	a := &App{db: db, logger: log.New(io.Discard, "", 0)}

	mock.ExpectQuery(`SELECT status, current_agent_id FROM tickets WHERE id = \$1`).
		WithArgs(int64(101)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "current_agent_id"}).AddRow("unassigned", nil))

	_, err = a.ResolveTicket(101)
	if err == nil {
		t.Fatalf("expected conflict error")
	}

	se, ok := err.(*statusError)
	if !ok {
		t.Fatalf("expected statusError, got %T", err)
	}
	if se.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, se.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestResolveTicket_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	a := &App{db: db, logger: log.New(io.Discard, "", 0)}

	mock.ExpectQuery(`SELECT status, current_agent_id FROM tickets WHERE id = \$1`).
		WithArgs(int64(200)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "current_agent_id"}).AddRow("assigned", int64(7)))

	mock.ExpectExec(`UPDATE tickets SET status = 'resolved', resolved_at = \$1, updated_at = \$2 WHERE id = \$3`).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), int64(200)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(`INSERT INTO assignments`).
		WithArgs(int64(200), sqlmock.AnyArg(), "resolved", "ticket marked resolved", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectQuery(`SELECT id, customer_email, category, language_preference, priority, created_at`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "customer_email", "category", "language_preference", "priority", "created_at"}))

	resp, err := a.ResolveTicket(200)
	if err != nil {
		t.Fatalf("ResolveTicket returned error: %v", err)
	}
	if resp.TicketID != 200 || resp.Status != "resolved" {
		t.Fatalf("unexpected response: %#v", resp)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestReopenTicket_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	a := &App{db: db, logger: log.New(io.Discard, "", 0)}

	mock.ExpectQuery(`SELECT status, current_agent_id, category, language_preference FROM tickets WHERE id = \$1`).
		WithArgs(int64(300)).
		WillReturnError(sql.ErrNoRows)

	_, err = a.ReopenTicket(300)
	if err == nil {
		t.Fatalf("expected not found error")
	}

	se, ok := err.(*statusError)
	if !ok {
		t.Fatalf("expected statusError, got %T", err)
	}
	if se.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, se.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestReopenTicket_StatusConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	a := &App{db: db, logger: log.New(io.Discard, "", 0)}

	mock.ExpectQuery(`SELECT status, current_agent_id, category, language_preference FROM tickets WHERE id = \$1`).
		WithArgs(int64(301)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "current_agent_id", "category", "language_preference"}).AddRow("assigned", nil, "billing", "english"))

	_, err = a.ReopenTicket(301)
	if err == nil {
		t.Fatalf("expected conflict error")
	}

	se, ok := err.(*statusError)
	if !ok {
		t.Fatalf("expected statusError, got %T", err)
	}
	if se.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, se.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestReopenTicket_SuccessWithoutPreviousAgentFallsBackToNormalRouting(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	a := &App{db: db, logger: log.New(io.Discard, "", 0)}

	mock.ExpectQuery(`SELECT status, current_agent_id, category, language_preference FROM tickets WHERE id = \$1`).
		WithArgs(int64(302)).
		WillReturnRows(sqlmock.NewRows([]string{"status", "current_agent_id", "category", "language_preference"}).AddRow("resolved", nil, "billing", "english"))

	mock.ExpectExec(`UPDATE tickets SET status = 'reopened', resolved_at = NULL, current_agent_id = NULL, assigned_at = NULL, updated_at = \$1 WHERE id = \$2`).
		WithArgs(sqlmock.AnyArg(), int64(302)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(`INSERT INTO assignments`).
		WithArgs(int64(302), nil, "reopened", "ticket reopened; system will try the previous agent first and then fallback to normal routing", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectBegin()

	mock.ExpectQuery(`SELECT status FROM tickets WHERE id = \$1 FOR UPDATE`).
		WithArgs(int64(302)).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("reopened"))

	mock.ExpectQuery(`SELECT id, skills, languages, shift_start_minutes, shift_end_minutes, max_capacity`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "skills", "languages", "shift_start_minutes", "shift_end_minutes", "max_capacity"}))

	mock.ExpectExec(`INSERT INTO assignments`).
		WithArgs(int64(302), nil, "pending", "no eligible agent available right now; ticket kept in unassigned queue", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit()

	resp, err := a.ReopenTicket(302)
	if err != nil {
		t.Fatalf("ReopenTicket returned error: %v", err)
	}
	if resp.TicketID != 302 || resp.Status != "reopened" {
		t.Fatalf("unexpected response: %#v", resp)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
