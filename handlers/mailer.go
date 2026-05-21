package handlers

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
)

func smtpConfig() (host string, port int, username, password, fromEmail, fromName string, err error) {
	host = strings.TrimSpace(os.Getenv("SMTP_HOST"))
	portStr := strings.TrimSpace(os.Getenv("SMTP_PORT"))
	username = strings.TrimSpace(os.Getenv("SMTP_USERNAME"))
	password = os.Getenv("SMTP_PASSWORD")
	fromEmail = strings.TrimSpace(os.Getenv("SMTP_FROM_EMAIL"))
	fromName = strings.TrimSpace(os.Getenv("SMTP_FROM_NAME"))
	if fromName == "" {
		fromName = "Vizzel Sales"
	}

	if host == "" || portStr == "" || fromEmail == "" {
		return "", 0, "", "", "", "", fmt.Errorf("SMTP ยังไม่ถูกตั้งค่า (SMTP_HOST/SMTP_PORT/SMTP_FROM_EMAIL)")
	}
	port, err = strconv.Atoi(portStr)
	if err != nil {
		return "", 0, "", "", "", "", fmt.Errorf("SMTP_PORT ไม่ถูกต้อง")
	}
	return host, port, username, password, fromEmail, fromName, nil
}

func sendSMTPMail(to, subject, textBody string) error {
	host, port, username, password, fromEmail, fromName, err := smtpConfig()
	if err != nil {
		return err
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	var msg bytes.Buffer
	msg.WriteString(fmt.Sprintf("From: %s <%s>\r\n", fromName, fromEmail))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(textBody)
	msg.WriteString("\r\n")

	var auth smtp.Auth
	if username != "" || password != "" {
		auth = smtp.PlainAuth("", username, password, host)
	}
	return smtp.SendMail(addr, auth, fromEmail, []string{to}, msg.Bytes())
}

func sendSMTPMailWithICS(to, subject, textBody, icsContent, fileName string) error {
	host, port, username, password, fromEmail, fromName, err := smtpConfig()
	if err != nil {
		return err
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	boundary := fmt.Sprintf("vizzel-boundary-%d", time.Now().UnixNano())

	var msg bytes.Buffer
	msg.WriteString(fmt.Sprintf("From: %s <%s>\r\n", fromName, fromEmail))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n", boundary))
	msg.WriteString("\r\n")

	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(textBody)
	msg.WriteString("\r\n")

	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/calendar; method=REQUEST; charset=UTF-8\r\n")
	msg.WriteString("Content-Transfer-Encoding: base64\r\n")
	msg.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", fileName))
	msg.WriteString("\r\n")

	b64 := base64.StdEncoding.EncodeToString([]byte(icsContent))
	for i := 0; i < len(b64); i += 76 {
		end := i + 76
		if end > len(b64) {
			end = len(b64)
		}
		msg.WriteString(b64[i:end] + "\r\n")
	}

	msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	var auth smtp.Auth
	if username != "" || password != "" {
		auth = smtp.PlainAuth("", username, password, host)
	}
	return smtp.SendMail(addr, auth, fromEmail, []string{to}, msg.Bytes())
}
