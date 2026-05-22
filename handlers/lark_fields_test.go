package handlers

import "testing"

func TestBuildLarkDetailNoteField(t *testing.T) {
	fields := buildLarkDetailNoteField("ติดต่อ ฝ่ายจัดซื้อ")
	if got := fields[larkDetailNoteColumnName()]; got != "ติดต่อ ฝ่ายจัดซื้อ" {
		t.Fatalf("detail note = %v", got)
	}
}

func TestBuildLarkDetailNoteFieldEmpty(t *testing.T) {
	fields := buildLarkDetailNoteField("  ")
	if got := fields[larkDetailNoteColumnName()]; got != "—" {
		t.Fatalf("empty detail note = %v, want em dash", got)
	}
}
