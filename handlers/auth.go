package handlers

import (
"context"
"encoding/json"
"fmt"
"net/http"
"os"
"time"

"github.com/gin-gonic/gin"
"github.com/golang-jwt/jwt/v5"
"vizzel-backend/config"
)

type LineAuthRequest struct {
AccessToken string `json:"access_token" binding:"required"`
}

type LineProfile struct {
UserID      string `json:"userId"`
DisplayName string `json:"displayName"`
PictureURL  string `json:"pictureUrl"`
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

db := config.DB
_, err = db.Exec(context.Background(), `
INSERT INTO users (line_id, full_name, created_at)
VALUES ($1, $2, NOW())
ON CONFLICT (line_id) DO UPDATE SET full_name = EXCLUDED.full_name
`, profile.UserID, profile.DisplayName)
if err != nil {
c.JSON(http.StatusInternalServerError, gin.H{"error": "db error: " + err.Error()})
return
}

claims := jwt.MapClaims{
"sub":  profile.UserID,
"name": profile.DisplayName,
"exp":  time.Now().Add(7 * 24 * time.Hour).Unix(),
}
token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
tokenStr, err := token.SignedString([]byte(os.Getenv("LINE_CHANNEL_SECRET")))
if err != nil {
c.JSON(http.StatusInternalServerError, gin.H{"error": "token signing failed"})
return
}

c.JSON(http.StatusOK, gin.H{
"token": tokenStr,
"user": gin.H{
"line_id": profile.UserID,
"name":    profile.DisplayName,
"picture": profile.PictureURL,
},
})
}

func fetchLineProfile(accessToken string) (*LineProfile, error) {
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
