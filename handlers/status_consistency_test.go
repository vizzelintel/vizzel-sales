package handlers

import (
	"testing"
	"time"
)

// fakeProjectRow implements the Scan(...any) error interface scanProject
// expects, without needing a real database row.
type fakeProjectRow struct{ status string }

func (f fakeProjectRow) Scan(dest ...any) error {
	vals := []any{
		"id-1", "company-1", "Test Agency", "type", "region", "person", "position", "phone",
		f.status, "note", "reject", "created-by", time.Now(),
		"appt-date", "appt-note", "cal-event", "present-type", "detail", "auto-reject", "lark-rec",
	}
	for i, d := range dest {
		switch v := d.(type) {
		case *string:
			*v = vals[i].(string)
		case *time.Time:
			*v = vals[i].(time.Time)
		}
	}
	return nil
}

// scanProject already normalizes legacy "present" rows to "register" on read
// — this is the correctness baseline the rest of BUG-07 builds on.
func TestScanProject_NormalizesPresentToRegister(t *testing.T) {
	p, err := scanProject(fakeProjectRow{status: "present"})
	if err != nil {
		t.Fatalf("scanProject: %v", err)
	}
	if p.Status != "register" {
		t.Fatalf("scanProject(status=present).Status = %q, want %q", p.Status, "register")
	}
}

// BUG-07: "present" is read back as normalized to "register" (see above), but
// validStatuses still accepts "present" as a status to write, and the Lark
// inbound label map still round-trips "present" -> "present". That lets
// UpdateProjectStatus and Lark-inbound writes keep reintroducing a status
// value that reads immediately normalize away, so status filters/counts and
// autoAdvanceStatus (which checks currentStatus == "present") disagree with
// what the API actually returns. validStatuses/larkLabelToStatus should not
// accept a status that scanProject treats as legacy. See BUG_REPORT.md BUG-07.
func TestValidStatuses_RejectsLegacyPresentStatus(t *testing.T) {
	if validStatuses["present"] {
		t.Fatalf(`validStatuses["present"] = true, want false — "present" is normalized to ` +
			`"register" on read (scanProject) but can still be written via UpdateProjectStatus, ` +
			`reintroducing the inconsistency (BUG-07)`)
	}
}

func TestLarkLabelToStatus_DoesNotRoundTripLegacyPresentStatus(t *testing.T) {
	if larkLabelToStatus["present"] == "present" {
		t.Fatalf(`larkLabelToStatus["present"] = "present" — Lark inbound sync can still set a ` +
			`project's status to "present", which the API then treats as legacy and displays as ` +
			`"register", desynchronizing Lark and the app (BUG-07)`)
	}
}
