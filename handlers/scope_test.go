package handlers

import "testing"

func TestNormalizeProjectsScope(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"pipeline", "pipeline"},
		{" directory ", "directory"},
		{"", "directory"},
		{"invalid", "directory"},
	}
	for _, tc := range tests {
		if got := normalizeProjectsScope(tc.in); got != tc.want {
			t.Errorf("normalizeProjectsScope(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
