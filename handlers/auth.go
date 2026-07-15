package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"vizzel-backend/config"
)

type LineAuthRequest struct {
	AccessToken string `json:"access_token" binding:"required"`
	Email       string `json:"email"` // optional — populated when LINE channel has email scope
}

type LineProfile struct {
	UserID      string `json:"userId"`
	DisplayName string `json:"displayName"`
	PictureURL  string `json:"pictureUrl"`
}

type RegisterRequest struct {
	AccessToken string `json:"access_token"  binding:"required"`
	DisplayName string `json:"display_name"`
	PictureURL  string `json:"picture_url"`
	FirstName   string `json:"first_name"   binding:"required"`
	LastName    string `json:"last_name"    binding:"required"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	Region      string `json:"region"`
	InviteCode  string `json:"invite_code"  binding:"required"`
}

func issueJWT(userID, lineID, displayName, role string) (string, error) {
	claims := jwt.MapClaims{
		"sub":  lineID,
		"uid":  userID,
		"name": displayName,
		"role": role,
		"exp":  time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	secret := config.JWTSecret()
	if secret == "" {
		return "", fmt.Errorf("JWT_SECRET is not set")
	}
	return t.SignedString([]byte(secret))
}

func LineLogin(c *gin.Context) {
	var req LineAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "access_token required"})
		return
	}

	profile, err := fetchLineProfile(req.AccessToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid LINE token: " + err.Error()})
		return
	}

	// Look up existing user by LINE ID
	var userID, userRole string
	err = config.DB.QueryRow(context.Background(),
		`SELECT id::text, COALESCE(role,'') FROM users WHERE line_id = $1`,
		profile.UserID,
	).Scan(&userID, &userRole)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// New user — frontend should show registration form
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":        "user_not_found",
				"line_id":      profile.UserID,
				"display_name": profile.DisplayName,
				"picture_url":  profile.PictureURL,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error: " + err.Error()})
		return
	}

	// Keep display_name and email in sync for existing users
	_, _ = config.DB.Exec(context.Background(),
		`UPDATE users SET full_name = $1, email = COALESCE(NULLIF($2,''), email) WHERE line_id = $3`,
		profile.DisplayName, req.Email, profile.UserID,
	)

	tokenStr, err := issueJWT(userID, profile.UserID, profile.DisplayName, userRole)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token signing failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": tokenStr,
		"user": gin.H{
			"id":      userID,
			"line_id": profile.UserID,
			"name":    profile.DisplayName,
			"picture": profile.PictureURL,
			"email":   req.Email,
			"role":    userRole,
		},
	})
}

// ValidateInviteCode — GET /api/v1/auth/validate-invite?code=XXX (no auth required)
func ValidateInviteCode(c *gin.Context) {
	code := strings.TrimSpace(c.Query("code"))
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code required"})
		return
	}

	var companyID, companyName string
	err := config.DB.QueryRow(context.Background(),
		`SELECT id::text, name FROM companies WHERE UPPER(TRIM(invite_code)) = UPPER(TRIM($1))`, code,
	).Scan(&companyID, &companyName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "invite code ไม่ถูกต้อง"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"company_id": companyID, "company_name": companyName})
}

// Register — POST /api/v1/auth/register (no auth required)
// The caller must prove ownership of the LINE account being registered by
// presenting a valid LINE access_token (verified via fetchLineProfile), the
// same way LineLogin does. The LINE id is always taken from that verified
// profile — never from client-supplied input — so an invite_code alone
// cannot be used to bind an account to someone else's LINE id.
func Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	profile, err := fetchLineProfile(req.AccessToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid LINE token: " + err.Error()})
		return
	}
	lineID := profile.UserID

	// Prevent duplicate registration
	var existingID string
	dupErr := config.DB.QueryRow(context.Background(),
		`SELECT id::text FROM users WHERE line_id = $1`, lineID,
	).Scan(&existingID)
	if dupErr == nil {
		// User already registered — just issue a new token
		var role string
		_ = config.DB.QueryRow(context.Background(),
			`SELECT COALESCE(role,'') FROM users WHERE id = $1::uuid`, existingID,
		).Scan(&role)
		tokenStr, err := issueJWT(existingID, lineID, profile.DisplayName, role)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "token signing failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"token": tokenStr})
		return
	}

	// Validate invite code
	var companyID string
	err = config.DB.QueryRow(context.Background(),
		`SELECT id::text FROM companies WHERE UPPER(TRIM(invite_code)) = UPPER(TRIM($1))`, req.InviteCode,
	).Scan(&companyID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invite code ไม่ถูกต้อง"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}

	fullName := strings.TrimSpace(req.FirstName + " " + req.LastName)

	var userID string
	err = config.DB.QueryRow(context.Background(),
		`INSERT INTO users (line_id, full_name, first_name, last_name, phone, email, region,
		                    role, company_id, invite_code_used)
		 VALUES ($1, $2, $3, $4, NULLIF($5,''), NULLIF($6,''), NULLIF($7,''),
		         'dealer', $8::uuid, $9)
		 RETURNING id::text`,
		lineID, fullName, req.FirstName, req.LastName,
		req.Phone, req.Email, req.Region,
		companyID, req.InviteCode,
	).Scan(&userID)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "users_role_check") {
			errMsg = "ระบบ role ในฐานข้อมูลยังไม่รองรับ dealer — รัน migration 005_pipeline_and_users_role.sql ใน Supabase"
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register: " + errMsg})
		return
	}

	tokenStr, err := issueJWT(userID, lineID, fullName, "dealer")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token signing failed"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"token": tokenStr})
}

// fetchLineProfile is a var (not a plain func) so tests can substitute a
// fake LINE API without making real network calls.
var fetchLineProfile = fetchLineProfileLive

func fetchLineProfileLive(accessToken string) (*LineProfile, error) {
	req, _ := http.NewRequest("GET", "https://api.line.me/v2/profile", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("LINE API returned %d", resp.StatusCode)
	}

	var p LineProfile
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}
