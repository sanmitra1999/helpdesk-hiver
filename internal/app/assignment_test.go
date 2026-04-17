package app

import (
	"io"
	"log"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func TestPendingAssignmentEngineSelectAgent_PrefersLanguageThenLeastLoad(t *testing.T) {
	agents := []*routingAgent{
		{
			id:             1,
			categorySkills: []string{"billing"},
			languages:      []string{"english"},
			maxCapacity:    3,
			currentLoad:    2,
			withinShift:    true,
		},
		{
			id:             2,
			categorySkills: []string{"billing"},
			languages:      []string{"english", "hindi"},
			maxCapacity:    3,
			currentLoad:    2,
			withinShift:    true,
		},
		{
			id:             3,
			categorySkills: []string{"billing"},
			languages:      []string{"hindi"},
			maxCapacity:    3,
			currentLoad:    1,
			withinShift:    true,
		},
	}

	engine := newPendingAssignmentEngine(agents)
	agent, ok := engine.selectAgent("billing", "english")
	if !ok {
		t.Fatalf("expected an agent to be selected")
	}
	if agent.id != 1 {
		t.Fatalf("expected agent id 1, got %d", agent.id)
	}
}

func TestPendingAssignmentEngineSelectAgent_FallsBackToSkillOnly(t *testing.T) {
	agents := []*routingAgent{
		{
			id:             11,
			categorySkills: []string{"technical"},
			languages:      []string{"french"},
			maxCapacity:    5,
			currentLoad:    2,
			withinShift:    true,
		},
		{
			id:             12,
			categorySkills: []string{"technical"},
			languages:      []string{"german"},
			maxCapacity:    5,
			currentLoad:    1,
			withinShift:    true,
		},
	}

	engine := newPendingAssignmentEngine(agents)
	agent, ok := engine.selectAgent("technical", "english")
	if !ok {
		t.Fatalf("expected skill-only fallback selection")
	}
	if agent.id != 12 {
		t.Fatalf("expected least-loaded skill match (id 12), got %d", agent.id)
	}
}

func TestAssignmentReason(t *testing.T) {
	preferred := assignmentReason(true, 1, 3, true)
	if !strings.Contains(preferred, "reopened ticket returned to previous agent") {
		t.Fatalf("expected preferred reason text, got %q", preferred)
	}
	if !strings.Contains(preferred, "preferred language matched") {
		t.Fatalf("expected language-match reason text, got %q", preferred)
	}

	fallback := assignmentReason(false, 2, 4, false)
	if !strings.Contains(fallback, "agent had the required skill") {
		t.Fatalf("expected skill reason text, got %q", fallback)
	}
	if !strings.Contains(fallback, "no language-matched agent was available") {
		t.Fatalf("expected language fallback reason text, got %q", fallback)
	}
}

func TestAssignTicketWithPreference_PrefersPreviousAgent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	a := &App{db: db, logger: log.New(io.Discard, "", 0)}
	preferredID := int64(7)
	now := time.Now().UTC()

	mock.ExpectBegin()

	mock.ExpectQuery(`SELECT status FROM tickets WHERE id = \$1 FOR UPDATE`).
		WithArgs(int64(2001)).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("reopened"))

	mock.ExpectQuery(`SELECT is_online, shift_start_minutes, shift_end_minutes, max_capacity, skills, languages`).
		WithArgs(preferredID).
		WillReturnRows(sqlmock.NewRows([]string{"is_online", "shift_start_minutes", "shift_end_minutes", "max_capacity", "skills", "languages"}).AddRow(true, 0, 0, 3, "{billing}", "{english,hindi}"))

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM tickets WHERE current_agent_id = \$1 AND status = 'assigned'`).
		WithArgs(preferredID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	mock.ExpectExec(`UPDATE tickets SET current_agent_id = \$1, status = 'assigned', assigned_at = \$2, updated_at = \$3 WHERE id = \$4`).
		WithArgs(preferredID, now, now, int64(2001)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(`UPDATE agents SET updated_at = \$1 WHERE id = \$2`).
		WithArgs(now, preferredID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectExec(`INSERT INTO assignments`).
		WithArgs(int64(2001), preferredID, "assigned", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit()

	assigned, agentID := a.assignTicketWithPreference(2001, "billing", "english", &preferredID, now)
	if !assigned {
		t.Fatalf("expected preferred assignment success")
	}
	if agentID == nil || *agentID != preferredID {
		t.Fatalf("expected preferred agent id %d, got %#v", preferredID, agentID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestAssignTicket_NoCandidatesAddsPendingEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	defer db.Close()

	a := &App{db: db, logger: log.New(io.Discard, "", 0)}
	now := time.Now().UTC()

	mock.ExpectBegin()

	mock.ExpectQuery(`SELECT status FROM tickets WHERE id = \$1 FOR UPDATE`).
		WithArgs(int64(3001)).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("unassigned"))

	mock.ExpectQuery(`SELECT id, skills, languages, shift_start_minutes, shift_end_minutes, max_capacity`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "skills", "languages", "shift_start_minutes", "shift_end_minutes", "max_capacity"}))

	mock.ExpectExec(`INSERT INTO assignments`).
		WithArgs(int64(3001), nil, "pending", "no eligible agent available right now; ticket kept in unassigned queue", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	mock.ExpectCommit()

	assigned, agentID := a.assignTicket(3001, "customer@example.com", "billing", "english", "high", now)
	if assigned {
		t.Fatalf("expected assignment to fail with no candidates")
	}
	if agentID != nil {
		t.Fatalf("expected nil agent id, got %#v", agentID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
