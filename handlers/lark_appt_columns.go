package handlers

import "fmt"

// Numbered Bitable columns (1–10 per type). Support edits slot N directly.

func larkColPresentDateN(n int) string   { return fmt.Sprintf("วันพรีเซ็น %d", n) }
func larkColPresentMeetN(n int) string   { return fmt.Sprintf("Meet พรีเซ็น %d", n) }
func larkColPresentNoteN(n int) string   { return fmt.Sprintf("หมายเหตุ พรีเซ็น %d", n) }
func larkColPresentTypeN(n int) string   { return fmt.Sprintf("รูปแบบ พรีเซ็น %d", n) }
func larkColDemoDateN(n int) string      { return fmt.Sprintf("วัน Demo %d", n) }
func larkColDemoNoteN(n int) string      { return fmt.Sprintf("หมายเหตุ Demo %d", n) }
func larkColSurveyDateN(n int) string    { return fmt.Sprintf("วัน Site Survey %d", n) }
func larkColSurveyNoteN(n int) string    { return fmt.Sprintf("หมายเหตุ Site Survey %d", n) }

func larkPresentDateColsN(n int) []string {
	if n == 1 {
		return []string{larkColPresentDateN(1), larkColApptPresentDate, "วันนัดพรีเซ็น"}
	}
	return []string{larkColPresentDateN(n)}
}

func larkPresentMeetColsN(n int) []string {
	if n == 1 {
		return []string{larkColPresentMeetN(1), larkColPresentMeetLink, "ลิงก์ Google Meet", "ลิงก์ Meet"}
	}
	return []string{larkColPresentMeetN(n)}
}

func larkPresentNoteColsN(n int) []string {
	if n == 1 {
		return []string{larkColPresentNoteN(1), larkColPresentNote}
	}
	return []string{larkColPresentNoteN(n)}
}

func larkPresentTypeColsN(n int) []string {
	if n == 1 {
		return []string{larkColPresentTypeN(1), larkColPresentType}
	}
	return []string{larkColPresentTypeN(n)}
}

func larkDemoDateColsN(n int) []string {
	if n == 1 {
		return []string{larkColDemoDateN(1), larkColApptDemoDate, "วันนัด Demo", "วันนัด demo"}
	}
	return []string{larkColDemoDateN(n)}
}

func larkDemoNoteColsN(n int) []string {
	if n == 1 {
		return []string{larkColDemoNoteN(1), larkColDemoNote}
	}
	return []string{larkColDemoNoteN(n)}
}

func larkSurveyDateColsN(n int) []string {
	if n == 1 {
		return []string{larkColSurveyDateN(1), larkColApptSurveyDate, "วันนัด Site Survey"}
	}
	return []string{larkColSurveyDateN(n)}
}

func larkSurveyNoteColsN(n int) []string {
	if n == 1 {
		return []string{larkColSurveyNoteN(1), larkColSurveyNote}
	}
	return []string{larkColSurveyNoteN(n)}
}

// RecommendedLarkAppointmentSlotColumns lists numbered appointment columns for Bitable setup.
func RecommendedLarkAppointmentSlotColumns() []string {
	var cols []string
	for n := 1; n <= maxAppointmentsPerType; n++ {
		cols = append(cols,
			larkColPresentDateN(n), larkColPresentMeetN(n), larkColPresentNoteN(n), larkColPresentTypeN(n),
			larkColDemoDateN(n), larkColDemoNoteN(n),
			larkColSurveyDateN(n), larkColSurveyNoteN(n),
		)
	}
	return cols
}
