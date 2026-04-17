package app

import (
	"reflect"
	"testing"
	"time"
)

func TestNormalizeList(t *testing.T) {
	in := []string{" Billing ", "billing", "GENERAL", "", " general "}
	got := normalizeList(in)
	want := []string{"billing", "general"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeList mismatch: got %#v want %#v", got, want)
	}
}

func TestNormalizeValue(t *testing.T) {
	if got := normalizeValue("  HeLLo "); got != "hello" {
		t.Fatalf("expected hello, got %q", got)
	}
	if got := normalizeValue("   "); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestParseHHMM(t *testing.T) {
	got, err := parseHHMM("09:30")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 570 {
		t.Fatalf("expected 570, got %d", got)
	}

	_, err = parseHHMM("25:99")
	if err == nil {
		t.Fatalf("expected parse error for invalid time")
	}
}

func TestWithinShift(t *testing.T) {
	tests := []struct {
		name  string
		start int
		end   int
		now   time.Time
		want  bool
	}{
		{
			name:  "same start end means always active",
			start: 600,
			end:   600,
			now:   time.Date(2026, 4, 17, 3, 0, 0, 0, time.UTC),
			want:  true,
		},
		{
			name:  "normal shift inside",
			start: 540,
			end:   1020,
			now:   time.Date(2026, 4, 17, 9, 0, 0, 0, time.UTC),
			want:  true,
		},
		{
			name:  "normal shift outside at end boundary",
			start: 540,
			end:   1020,
			now:   time.Date(2026, 4, 17, 17, 0, 0, 0, time.UTC),
			want:  false,
		},
		{
			name:  "overnight shift before midnight",
			start: 1320,
			end:   360,
			now:   time.Date(2026, 4, 17, 23, 0, 0, 0, time.UTC),
			want:  true,
		},
		{
			name:  "overnight shift after midnight",
			start: 1320,
			end:   360,
			now:   time.Date(2026, 4, 17, 2, 0, 0, 0, time.UTC),
			want:  true,
		},
		{
			name:  "overnight shift outside",
			start: 1320,
			end:   360,
			now:   time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := withinShift(tc.start, tc.end, tc.now)
			if got != tc.want {
				t.Fatalf("withinShift(%d,%d,%s) got %v want %v", tc.start, tc.end, tc.now.Format(time.RFC3339), got, tc.want)
			}
		})
	}
}

func TestIsValidPriority(t *testing.T) {
	valid := []string{"low", "medium", "high", "urgent"}
	for _, p := range valid {
		if !isValidPriority(p) {
			t.Fatalf("expected %q to be valid", p)
		}
	}

	invalid := []string{"", "critical", "LOW"}
	for _, p := range invalid {
		if isValidPriority(p) {
			t.Fatalf("expected %q to be invalid", p)
		}
	}
}

func TestPriorityRank(t *testing.T) {
	cases := map[string]int{
		"urgent": 4,
		"high":   3,
		"medium": 2,
		"low":    1,
		"other":  0,
	}

	for in, want := range cases {
		if got := priorityRank(in); got != want {
			t.Fatalf("priorityRank(%q) got %d want %d", in, got, want)
		}
	}
}

func TestContains(t *testing.T) {
	values := []string{"billing", "general"}
	if !contains(values, "billing") {
		t.Fatalf("expected contains to return true")
	}
	if contains(values, "technical") {
		t.Fatalf("expected contains to return false")
	}
}

func TestSplitPath(t *testing.T) {
	if got := splitPath("/"); got != nil {
		t.Fatalf("expected nil for root path, got %#v", got)
	}

	got := splitPath("/tickets/123/resolve/")
	want := []string{"tickets", "123", "resolve"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitPath mismatch: got %#v want %#v", got, want)
	}
}
