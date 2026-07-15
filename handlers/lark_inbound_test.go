package handlers

import (
	"testing"
	"time"
)

func TestLarkFieldText(t *testing.T) {
	if got := larkFieldText(map[string]interface{}{"text": "  hello  "}); got != "hello" {
		t.Fatalf("got %q", got)
	}
	if got := larkFieldText("plain"); got != "plain" {
		t.Fatalf("got %q", got)
	}
}

func TestStatusFromLarkLabel(t *testing.T) {
	// "present" is a legacy status normalized to "register" (BUG-07), so the
	// Lark "Present" label now resolves to "register".
	if got := statusFromLarkLabel("Present"); got != "register" {
		t.Fatalf("got %q", got)
	}
	if got := statusFromLarkLabel("Quotation"); got != "quotation" {
		t.Fatalf("got %q", got)
	}
	if got := statusFromLarkLabel(""); got != "" {
		t.Fatalf("empty")
	}
}

func TestParseLarkFieldsToPatch(t *testing.T) {
	fields := map[string]interface{}{
		"ชื่อหน่วยงาน":        "โรงพยาบาลทดสอบ",
		"สถานะ":             "Present",
		"Project ID":        "abc-123",
		"รายละเอียดเพิ่มเติม": "บันทึกจาก Lark",
	}
	p := parseLarkFieldsToPatch(fields)
	if p.AgencyName != "โรงพยาบาลทดสอบ" || p.Status != "register" || p.ProjectID != "abc-123" {
		t.Fatalf("%+v", p)
	}
	if !p.SetDetailNote || p.DetailNote != "บันทึกจาก Lark" {
		t.Fatalf("detail note %+v", p)
	}
}

func TestParseInboundSlotPresentMeet(t *testing.T) {
	fields := map[string]interface{}{
		larkColPresentMeetN(2): "https://meet.google.com/abc-defg-hij",
	}
	slot := parseInboundSlot(fields, 2, larkPresentDateColsN(2), larkPresentMeetColsN(2), larkPresentNoteColsN(2), larkPresentTypeColsN(2))
	if !slot.touched || slot.MeetLink != "https://meet.google.com/abc-defg-hij" {
		t.Fatalf("got %+v", slot)
	}
}

func TestParseLarkBitableDateMillis(t *testing.T) {
	ms := float64(time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC).UnixMilli())
	tm, ok := parseLarkBitableDate(ms)
	if !ok || tm.UnixMilli() != int64(ms) {
		t.Fatalf("got %v ok=%v", tm, ok)
	}
}
