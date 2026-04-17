package app

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

func newRouteTestApp(t *testing.T) (*App, sqlmock.Sqlmock, func()) {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}

	a := &App{db: db, logger: log.New(io.Discard, "", 0)}
	cleanup := func() { _ = db.Close() }
	return a, mock, cleanup
}

func TestRoutes_Healthz_MethodNotAllowed(t *testing.T) {
	a, _, cleanup := newRouteTestApp(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	rr := httptest.NewRecorder()
	a.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}
}

func TestRoutes_Healthz_OK(t *testing.T) {
	a, mock, cleanup := newRouteTestApp(t)
	defer cleanup()

	mock.ExpectPing()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	a.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestRoutes_Agents_InvalidJSON(t *testing.T) {
	a, _, cleanup := newRouteTestApp(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader("{"))
	rr := httptest.NewRecorder()
	a.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestRoutes_Agents_CreateValidationError(t *testing.T) {
	a, _, cleanup := newRouteTestApp(t)
	defer cleanup()

	body := `{"name":"","email":"","skills":[],"languages":[],"shift_start_utc":"09:00","shift_end_utc":"17:00","max_capacity":1,"is_online":false}`
	req := httptest.NewRequest(http.MethodPost, "/agents", strings.NewReader(body))
	rr := httptest.NewRecorder()
	a.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestRoutes_AgentsSubroutes_InvalidID(t *testing.T) {
	a, _, cleanup := newRouteTestApp(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPatch, "/agents/abc/status", strings.NewReader(`{"online":true}`))
	rr := httptest.NewRecorder()
	a.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestRoutes_Tickets_MethodNotAllowed(t *testing.T) {
	a, _, cleanup := newRouteTestApp(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/tickets", nil)
	rr := httptest.NewRecorder()
	a.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}
}

func TestRoutes_TicketsSubroutes_InvalidID(t *testing.T) {
	a, _, cleanup := newRouteTestApp(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/tickets/xyz", nil)
	rr := httptest.NewRecorder()
	a.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestRoutes_Tickets_CreateValidationError(t *testing.T) {
	a, _, cleanup := newRouteTestApp(t)
	defer cleanup()

	body := `{"customer_name":"","customer_email":"","category":"","language_preference":"","priority":""}`
	req := httptest.NewRequest(http.MethodPost, "/tickets", strings.NewReader(body))
	rr := httptest.NewRecorder()
	a.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestRoutes_AssignmentSummary_MethodNotAllowed(t *testing.T) {
	a, _, cleanup := newRouteTestApp(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/assignments/summary", nil)
	rr := httptest.NewRecorder()
	a.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}
}
