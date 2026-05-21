package handlers

import (
	"fmt"
	"strings"
	"time"
)

func toICSDateTime(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

func buildICSInvite(uid, title, description, location string, start, end time.Time) string {
	safe := func(s string) string {
		s = strings.ReplaceAll(s, "\\", "\\\\")
		s = strings.ReplaceAll(s, "\n", "\\n")
		s = strings.ReplaceAll(s, ",", "\\,")
		s = strings.ReplaceAll(s, ";", "\\;")
		return s
	}
	now := toICSDateTime(time.Now().UTC())
	return strings.Join([]string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//Vizzel Sales//Calendar Invite//TH",
		"CALSCALE:GREGORIAN",
		"METHOD:REQUEST",
		"BEGIN:VEVENT",
		"UID:" + safe(uid),
		"DTSTAMP:" + now,
		"DTSTART:" + toICSDateTime(start),
		"DTEND:" + toICSDateTime(end),
		"SUMMARY:" + safe(title),
		"DESCRIPTION:" + safe(description),
		"LOCATION:" + safe(location),
		"STATUS:CONFIRMED",
		"SEQUENCE:0",
		"END:VEVENT",
		"END:VCALENDAR",
		"",
	}, "\r\n")
}

func SendCalendarInviteEmail(toEmail, agencyName, statusLabel, note string, start time.Time, projectID string) error {
	if strings.TrimSpace(toEmail) == "" {
		return fmt.Errorf("ไม่พบอีเมลผู้ใช้งาน")
	}
	end := start.Add(time.Hour)
	subject := fmt.Sprintf("[Vizzel] นัดหมาย%s - %s", statusLabel, agencyName)
	textBody := fmt.Sprintf(
		"มีการนัดหมาย %s สำหรับโครงการ %s\nเวลา: %s\n\nหมายเหตุ: %s\n\nสามารถกดเปิดไฟล์แนบ .ics เพื่อนำเข้าปฏิทิน (Google/Outlook/Apple) ได้ทันที",
		statusLabel,
		agencyName,
		start.In(time.FixedZone("ICT", 7*3600)).Format("02/01/2006 15:04"),
		note,
	)
	uid := fmt.Sprintf("%s-%s-%d@vizzel.sales", projectID, strings.ToLower(statusLabel), start.Unix())
	ics := buildICSInvite(uid, subject, textBody, "Thailand", start, end)
	return sendSMTPMailWithICS(toEmail, subject, textBody, ics, "invite.ics")
}
