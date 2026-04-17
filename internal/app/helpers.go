package app

import (
	"sort"
	"strings"
	"time"
)

// normalizeList removes duplicates from a slice of strings, normalizes each value
// (lowercasing and trimming whitespace), sorts the result, and returns the cleaned list.
func normalizeList(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, v := range values {
		n := normalizeValue(v)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// normalizeValue converts a string to lowercase and trims whitespace.
// Returns an empty string if the result is empty after trimming.
func normalizeValue(v string) string { return strings.ToLower(strings.TrimSpace(v)) }

// parseHHMM parses a time string in "HH:MM" format and converts it to minutes since midnight.
// Returns the total minutes and any parsing error.
func parseHHMM(v string) (int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(v))
	if err != nil {
		return 0, err
	}
	return t.Hour()*60 + t.Minute(), nil
}

// withinShift checks if the current time falls within an agent's shift.
// Parameters:
//   - startMinutes: shift start time in minutes since midnight
//   - endMinutes: shift end time in minutes since midnight
//   - now: current time to check
//
// Returns true if the current time is within the shift period.
// Handles overnight shifts where endMinutes < startMinutes.
func withinShift(startMinutes, endMinutes int, now time.Time) bool {
	nowMinutes := now.Hour()*60 + now.Minute()
	if startMinutes == endMinutes {
		return true
	}
	if startMinutes < endMinutes {
		return nowMinutes >= startMinutes && nowMinutes < endMinutes
	}
	return nowMinutes >= startMinutes || nowMinutes < endMinutes
}

// isValidPriority checks if the given string is a valid priority level.
// Valid priorities are: "low", "medium", "high", "urgent".
// Returns true if valid, false otherwise.
func isValidPriority(v string) bool {
	switch v {
	case "low", "medium", "high", "urgent":
		return true
	default:
		return false
	}
}

// priorityRank returns a numeric ranking for priority levels.
// Higher numbers indicate higher priority:
//   - urgent: 4
//   - high: 3
//   - medium: 2
//   - low: 1
//   - invalid: 0
//
// This is used for sorting tickets by priority during assignment.
func priorityRank(v string) int {
	switch v {
	case "urgent":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

// contains checks if a slice of strings contains a specific target string.
// Returns true if the target is found, false otherwise.
// This is a simple linear search utility used for skill and language matching.
func contains(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

// splitPath splits a URL path into segments, trimming leading/trailing slashes.
// Returns nil for root path ("/"), otherwise returns path segments.
// This is used for parsing API endpoint paths.
func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}
