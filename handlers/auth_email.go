package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"vizzel-backend/config"
)

var emailRegex = regexp.MustCompile(`^[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$`)

type sendEmailOTPRequest struct {
	Email string `json:"email"`
}

type verifyEmailOTPRequest struct {
	Email string `json:"email" binding:"required"`
	OTP   string `json:"otp" binding:"required"`
}

func otpSecret() string {
	s := os.Getenv("EMAIL_OTP_SECRET")
	if strings.TrimSpace(s) == "" {
		s = os.Getenv("LINE_CHANNEL_SECRET")
	}
	return s
}

func hashOTP(email, otp string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email)) + ":" + otp + ":" + otpSecret()))
	return hex.EncodeToString(sum[:])
}

func generateOTP() (string, error) {
	const digits = "0123456789"
	buf := make([]byte, 6)
	rnd := make([]byte, 6)
	if _, err := rand.Read(rnd); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = digits[int(rnd[i])%10]
	}
	return string(buf), nil
}

func SendEmailOTP(c *gin.Context) {
	var req sendEmailOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)
	email := strings.TrimSpace(req.Email)

	var currentEmail string
	_ = config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(email,'') FROM users WHERE id = $1::uuid`, userIDStr,
	).Scan(&currentEmail)
	if email == "" {
		email = strings.TrimSpace(currentEmail)
	}
	if email == "" || !emailRegex.MatchString(email) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณากรอกอีเมลให้ถูกต้อง"})
		return
	}

	var resendAt time.Time
	checkErr := config.DB.QueryRow(context.Background(),
		`SELECT resend_available_at
		 FROM email_verifications
		 WHERE user_id = $1::uuid AND email = $2 AND verified_at IS NULL
		 ORDER BY created_at DESC
		 LIMIT 1`,
		userIDStr, email,
	).Scan(&resendAt)
	if checkErr == nil && time.Now().Before(resendAt) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "ส่งรหัสบ่อยเกินไป กรุณารอสักครู่"})
		return
	}

	otp, err := generateOTP()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถสร้างรหัส OTP ได้"})
		return
	}
	now := time.Now().UTC()
	expiresAt := now.Add(10 * time.Minute)
	resendAvailableAt := now.Add(60 * time.Second)
	otpHash := hashOTP(email, otp)

	tx, err := config.DB.Begin(context.Background())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	defer tx.Rollback(context.Background())

	if _, err := tx.Exec(context.Background(),
		`UPDATE users
		 SET email = $1, email_verified_at = NULL
		 WHERE id = $2::uuid`,
		email, userIDStr,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถบันทึกอีเมลได้"})
		return
	}

	if _, err := tx.Exec(context.Background(),
		`INSERT INTO email_verifications (user_id, email, otp_hash, expires_at, resend_available_at)
		 VALUES ($1::uuid, $2, $3, $4, $5)`,
		userIDStr, email, otpHash, expiresAt, resendAvailableAt,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถบันทึกรหัส OTP ได้"})
		return
	}

	subject := "รหัสยืนยันอีเมล Vizzel Sales"
	body := fmt.Sprintf("รหัส OTP ของคุณคือ %s\n\nรหัสนี้หมดอายุภายใน 10 นาที\n\nหากคุณไม่ได้เป็นผู้ร้องขอ กรุณาเพิกเฉยอีเมลนี้", otp)
	if err := sendSMTPMail(email, subject, body); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "ส่งอีเมลไม่สำเร็จ: " + err.Error()})
		return
	}

	if err := tx.Commit(context.Background()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db commit error"})
		return
	}

	resp := gin.H{"message": "ส่งรหัส OTP ไปยังอีเมลแล้ว", "email": email}
	if strings.EqualFold(os.Getenv("EMAIL_OTP_DEBUG"), "true") {
		resp["debug_otp"] = otp
	}
	c.JSON(http.StatusOK, resp)
}

func VerifyEmailOTP(c *gin.Context) {
	var req verifyEmailOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)
	email := strings.TrimSpace(req.Email)
	otp := strings.TrimSpace(req.OTP)
	if !emailRegex.MatchString(email) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "อีเมลไม่ถูกต้อง"})
		return
	}
	if len(otp) != 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OTP ต้องมี 6 หลัก"})
		return
	}

	var id, otpHash string
	var attempts int
	var expiresAt time.Time
	err := config.DB.QueryRow(context.Background(),
		`SELECT id::text, otp_hash, attempts, expires_at
		 FROM email_verifications
		 WHERE user_id = $1::uuid AND email = $2 AND verified_at IS NULL
		 ORDER BY created_at DESC
		 LIMIT 1`,
		userIDStr, email,
	).Scan(&id, &otpHash, &attempts, &expiresAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ไม่พบ OTP กรุณากดส่งรหัสใหม่"})
		return
	}
	if attempts >= 5 {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "กรอก OTP ผิดหลายครั้ง กรุณาส่งรหัสใหม่"})
		return
	}
	if time.Now().After(expiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "OTP หมดอายุแล้ว กรุณาส่งรหัสใหม่"})
		return
	}

	expected := hashOTP(email, otp)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(otpHash)) != 1 {
		_, _ = config.DB.Exec(context.Background(),
			`UPDATE email_verifications SET attempts = attempts + 1 WHERE id = $1::uuid`,
			id,
		)
		c.JSON(http.StatusBadRequest, gin.H{"error": "OTP ไม่ถูกต้อง"})
		return
	}

	tx, err := config.DB.Begin(context.Background())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	defer tx.Rollback(context.Background())

	if _, err := tx.Exec(context.Background(),
		`UPDATE email_verifications SET verified_at = NOW() WHERE id = $1::uuid`,
		id,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถยืนยัน OTP ได้"})
		return
	}
	if _, err := tx.Exec(context.Background(),
		`UPDATE users
		 SET email = $1, email_verified_at = NOW()
		 WHERE id = $2::uuid`,
		email, userIDStr,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ไม่สามารถอัปเดตสถานะอีเมลได้"})
		return
	}
	if err := tx.Commit(context.Background()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db commit error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "ยืนยันอีเมลสำเร็จ", "email_verified": true})
}
