package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"vizzel-backend/config"
)

func GetMe(c *gin.Context) {
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	type meResult struct {
		ID            string    `json:"id"`
		LineID        string    `json:"line_id"`
		FullName      string    `json:"full_name"`
		FirstName     string    `json:"first_name"`
		LastName      string    `json:"last_name"`
		Role          string    `json:"role"`
		Email         string    `json:"email"`
		Phone         string    `json:"phone"`
		Region        string    `json:"region"`
		CompanyID     string    `json:"company_id"`
		CompanyName   string    `json:"company_name"`
		EmailVerified bool      `json:"email_verified"`
		CreatedAt     time.Time `json:"created_at"`
	}

	var u meResult
	err := config.DB.QueryRow(context.Background(),
		`SELECT u.id::text, u.line_id,
		        COALESCE(u.full_name,''), COALESCE(u.first_name,''), COALESCE(u.last_name,''),
		        COALESCE(u.role,''), COALESCE(u.email,''), COALESCE(u.phone,''), COALESCE(u.region,''),
		        COALESCE(u.company_id::text,''), COALESCE(c.name,''),
		        (u.email_verified_at IS NOT NULL), u.created_at
		 FROM users u
		 LEFT JOIN companies c ON c.id = u.company_id
		 WHERE u.id = $1::uuid`, userIDStr,
	).Scan(&u.ID, &u.LineID, &u.FullName, &u.FirstName, &u.LastName,
		&u.Role, &u.Email, &u.Phone, &u.Region, &u.CompanyID, &u.CompanyName, &u.EmailVerified, &u.CreatedAt)

	if err != nil {
		// Fallback to line_id from JWT claim
		lineID, _ := c.Get("line_id")
		err = config.DB.QueryRow(context.Background(),
			`SELECT u.id::text, u.line_id,
			        COALESCE(u.full_name,''), COALESCE(u.first_name,''), COALESCE(u.last_name,''),
			        COALESCE(u.role,''), COALESCE(u.email,''), COALESCE(u.phone,''), COALESCE(u.region,''),
			        COALESCE(u.company_id::text,''), COALESCE(c.name,''),
			        (u.email_verified_at IS NOT NULL), u.created_at
			 FROM users u
			 LEFT JOIN companies c ON c.id = u.company_id
			 WHERE u.line_id = $1`, lineID,
		).Scan(&u.ID, &u.LineID, &u.FullName, &u.FirstName, &u.LastName,
			&u.Role, &u.Email, &u.Phone, &u.Region, &u.CompanyID, &u.CompanyName, &u.EmailVerified, &u.CreatedAt)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
	}

	c.JSON(http.StatusOK, u)
}

func UpdateMe(c *gin.Context) {
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	var req struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Phone     string `json:"phone"`
		Email     string `json:"email"`
		Region    string `json:"region"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Email changes must go through OTP flow (/auth/email/send-otp + verify-otp).
	if strings.TrimSpace(req.Email) != "" {
		var currentEmail string
		_ = config.DB.QueryRow(context.Background(),
			`SELECT COALESCE(email,'') FROM users WHERE id = $1::uuid`, userIDStr,
		).Scan(&currentEmail)
		if strings.TrimSpace(req.Email) != strings.TrimSpace(currentEmail) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "เปลี่ยนอีเมลต้องยืนยัน OTP ก่อน กรุณากดส่งรหัส OTP ในหน้าโปรไฟล์",
			})
			return
		}
	}

	fullName := strings.TrimSpace(req.FirstName + " " + req.LastName)
	if fullName == "" {
		fullName = strings.TrimSpace(req.FirstName)
	}

	_, err := config.DB.Exec(context.Background(),
		`UPDATE users
		 SET first_name = $1,
		     last_name  = $2,
		     full_name  = CASE WHEN $3 <> '' THEN $3 ELSE full_name END,
		     phone      = $4,
		     region     = $5
		 WHERE id = $6::uuid`,
		req.FirstName, req.LastName, fullName, req.Phone, req.Region, userIDStr,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update profile"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "อัปเดตโปรไฟล์สำเร็จ"})
}
