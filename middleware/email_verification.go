package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"vizzel-backend/config"
)

func EmailVerifiedGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.FullPath()
		// Allow user to view/update profile and complete verification flow.
		if path == "/api/v1/me" || path == "/api/v1/users/me" ||
			path == "/api/v1/auth/email/send-otp" || path == "/api/v1/auth/email/verify-otp" {
			c.Next()
			return
		}

		userID, _ := c.Get("user_id")
		userIDStr, _ := userID.(string)
		if strings.TrimSpace(userIDStr) == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid user"})
			return
		}

		var email, verifiedAt string
		err := config.DB.QueryRow(context.Background(),
			`SELECT COALESCE(email,''), COALESCE(email_verified_at::text,'')
			 FROM users WHERE id = $1::uuid`,
			userIDStr,
		).Scan(&email, &verifiedAt)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
			return
		}
		if verifiedAt == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "email_not_verified",
				"message": "กรุณายืนยันอีเมลก่อนใช้งาน",
				"email":   email,
			})
			return
		}

		c.Next()
	}
}
