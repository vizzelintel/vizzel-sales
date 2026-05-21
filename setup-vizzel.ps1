# ============================================================
# setup-vizzel.ps1
# รัน script นี้ใน PowerShell จาก folder vizzel-backend
# คำสั่ง: cd C:\Users\Acer\vizzel-backend; .\setup-vizzel.ps1
# ============================================================

Write-Host "=== Vizzel Backend Auto-Setup ===" -ForegroundColor Cyan

# ---------- config/database.go ----------
$dbConfig = @'
package config

import (
	"context"
	"log"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

var DB *pgxpool.Pool

func InitDB() {
	url := os.Getenv("SUPABASE_DB_URL")
	if url == "" {
		log.Fatal("Database connection failed: SUPABASE_DB_URL environment variable is not set")
	}

	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		log.Fatalf("Database connection failed: failed to ping database: %v", err)
	}

	DB = pool
	log.Println("Connected to Supabase PostgreSQL")
}

func GetDB() *pgxpool.Pool {
	return DB
}
'@
[System.IO.File]::WriteAllText("$PWD\config\database.go", $dbConfig)
Write-Host "[1/7] config/database.go" -ForegroundColor Green

# ---------- middleware/auth.go ----------
$middleware = @'
package middleware

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		secret := []byte(os.Getenv("LINE_CHANNEL_SECRET"))

		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return secret, nil
		})

		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid claims"})
			return
		}

		c.Set("line_id", claims["sub"])
		c.Set("name", claims["name"])
		c.Next()
	}
}
'@
New-Item -ItemType Directory -Force -Path "middleware" | Out-Null
[System.IO.File]::WriteAllText("$PWD\middleware\auth.go", $middleware)
Write-Host "[2/7] middleware/auth.go" -ForegroundColor Green

# ---------- handlers/webhook.go ----------
$webhook = @'
package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

type WebhookBody struct {
	Events []WebhookEvent `json:"events"`
}

type WebhookEvent struct {
	Type       string        `json:"type"`
	ReplyToken string        `json:"replyToken"`
	Source     WebhookSource `json:"source"`
	Message    WebhookMsg    `json:"message"`
}

type WebhookSource struct {
	Type   string `json:"type"`
	UserID string `json:"userId"`
}

type WebhookMsg struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func HandleWebhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read body"})
		return
	}

	sig := c.GetHeader("X-Line-Signature")
	if !verifyLineSignature(body, sig) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid signature"})
		return
	}

	var wb WebhookBody
	if err := json.Unmarshal(body, &wb); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}

	for _, event := range wb.Events {
		switch event.Type {
		case "message":
			if event.Message.Type == "text" {
				// TODO: handle incoming text
			}
		case "follow":
			// TODO: welcome message
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func verifyLineSignature(body []byte, signature string) bool {
	secret := os.Getenv("LINE_CHANNEL_SECRET")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return expected == signature
}
'@
[System.IO.File]::WriteAllText("$PWD\handlers\webhook.go", $webhook)
Write-Host "[3/7] handlers/webhook.go" -ForegroundColor Green

# ---------- handlers/auth.go ----------
$auth = @'
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
'@
[System.IO.File]::WriteAllText("$PWD\handlers\auth.go", $auth)
Write-Host "[4/7] handlers/auth.go" -ForegroundColor Green

# ---------- handlers/users.go ----------
$users = @'
package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"vizzel-backend/config"
)

func GetMe(c *gin.Context) {
	lineID, _ := c.Get("line_id")

	db := config.DB
	row := db.QueryRow(context.Background(), `
		SELECT id::text, line_id, COALESCE(full_name,''), COALESCE(role,''), COALESCE(email,'')
		FROM users WHERE line_id = $1
	`, lineID)

	var user struct {
		ID       string `json:"id"`
		LineID   string `json:"line_id"`
		FullName string `json:"full_name"`
		Role     string `json:"role"`
		Email    string `json:"email"`
	}

	if err := row.Scan(&user.ID, &user.LineID, &user.FullName, &user.Role, &user.Email); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	c.JSON(http.StatusOK, user)
}
'@
[System.IO.File]::WriteAllText("$PWD\handlers\users.go", $users)
Write-Host "[5/7] handlers/users.go" -ForegroundColor Green

# ---------- main.go ----------
$main = @'
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"vizzel-backend/config"
	"vizzel-backend/handlers"
	"vizzel-backend/middleware"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, reading from environment")
	}

	config.InitDB()

	r := gin.Default()

	// CORS
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// Public routes
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"service": "vizzel-backend", "status": "ok"})
	})
	r.POST("/api/v1/webhook", handlers.HandleWebhook)
	r.POST("/api/v1/auth/line", handlers.LineLogin)

	// Protected routes (JWT required)
	api := r.Group("/api/v1")
	api.Use(middleware.JWTAuth())
	{
		api.GET("/users/me", handlers.GetMe)
		api.POST("/projects", handlers.CreateProject)
		api.GET("/projects", handlers.GetProjects)
		api.POST("/documents", handlers.CreateDocument)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("vizzel-backend listening on :%s", port)
	r.Run(":" + port)
}
'@
[System.IO.File]::WriteAllText("$PWD\main.go", $main)
Write-Host "[6/7] main.go" -ForegroundColor Green

# ---------- liff/index.html ----------
New-Item -ItemType Directory -Force -Path "liff" | Out-Null
$liff = @'
<!DOCTYPE html>
<html lang="th">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0">
  <title>Vizzel Sales</title>
  <script charset="utf-8" src="https://static.line-scdn.net/liff/edge/2/sdk.js"></script>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body { font-family: 'Sarabun', sans-serif; background: #f5f5f5; color: #333; }
    .header { background: #06C755; color: white; padding: 16px 20px; display: flex; align-items: center; gap: 12px; }
    .header img { width: 36px; height: 36px; border-radius: 50%; }
    .header h1 { font-size: 18px; font-weight: 600; }
    .content { padding: 20px; }
    .card { background: white; border-radius: 12px; padding: 16px; margin-bottom: 16px; box-shadow: 0 1px 4px rgba(0,0,0,0.08); }
    .card h2 { font-size: 15px; color: #666; margin-bottom: 12px; }
    .btn { display: block; width: 100%; padding: 14px; background: #06C755; color: white; border: none; border-radius: 8px; font-size: 16px; font-weight: 600; cursor: pointer; text-align: center; margin-top: 8px; }
    .btn:active { opacity: 0.85; }
    .btn-outline { background: white; color: #06C755; border: 2px solid #06C755; }
    .project-item { padding: 12px 0; border-bottom: 1px solid #f0f0f0; }
    .project-item:last-child { border-bottom: none; }
    .project-item .name { font-weight: 600; font-size: 15px; }
    .project-item .sub { font-size: 13px; color: #999; margin-top: 4px; }
    .badge { display: inline-block; padding: 2px 8px; border-radius: 99px; font-size: 12px; font-weight: 600; }
    .badge-active { background: #e8faf0; color: #06C755; }
    .badge-pending { background: #fff8e6; color: #f5a623; }
    .loading { text-align: center; padding: 40px; color: #999; }
    #loginScreen { display: flex; flex-direction: column; align-items: center; justify-content: center; min-height: 100vh; padding: 40px 20px; text-align: center; }
    #loginScreen .logo { font-size: 48px; margin-bottom: 16px; }
    #loginScreen h2 { font-size: 22px; font-weight: 700; margin-bottom: 8px; }
    #loginScreen p { color: #666; margin-bottom: 32px; }
    #app { display: none; }
  </style>
</head>
<body>

<div id="loginScreen">
  <div class="logo">📊</div>
  <h2>Vizzel Sales</h2>
  <p>ระบบจัดการลูกค้าหน่วยงานราชการ</p>
  <button class="btn" onclick="doLogin()">เข้าสู่ระบบด้วย LINE</button>
</div>

<div id="app">
  <div class="header">
    <img id="userPic" src="" alt="">
    <div>
      <h1>สวัสดี, <span id="userName"></span></h1>
      <div style="font-size:12px;opacity:0.85">Vizzel Sales Dashboard</div>
    </div>
  </div>
  <div class="content">
    <div class="card">
      <h2>📋 โครงการล่าสุด</h2>
      <div id="projectList"><div class="loading">กำลังโหลด...</div></div>
      <button class="btn btn-outline" style="margin-top:12px" onclick="showCreateProject()">+ เพิ่มโครงการใหม่</button>
    </div>
  </div>
</div>

<script>
const LIFF_ID = "YOUR_LIFF_ID_HERE"; // ← เปลี่ยนเป็น LIFF ID จริง
const API = "https://vizzel-sales-api.fly.dev";
let token = "";

async function init() {
  await liff.init({ liffId: LIFF_ID });
  if (!liff.isLoggedIn()) {
    document.getElementById("loginScreen").style.display = "flex";
    return;
  }
  await loginWithLine();
}

async function doLogin() {
  liff.login();
}

async function loginWithLine() {
  try {
    const accessToken = liff.getAccessToken();
    const profile = await liff.getProfile();

    document.getElementById("userName").textContent = profile.displayName;
    document.getElementById("userPic").src = profile.pictureUrl;

    const res = await fetch(`${API}/api/v1/auth/line`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ access_token: accessToken })
    });
    const data = await res.json();
    token = data.token;

    document.getElementById("loginScreen").style.display = "none";
    document.getElementById("app").style.display = "block";

    loadProjects();
  } catch (e) {
    alert("Login failed: " + e.message);
  }
}

async function loadProjects() {
  try {
    const res = await fetch(`${API}/api/v1/projects`, {
      headers: { "Authorization": "Bearer " + token }
    });
    const projects = await res.json();
    const list = document.getElementById("projectList");

    if (!projects || projects.length === 0) {
      list.innerHTML = '<div class="loading">ยังไม่มีโครงการ</div>';
      return;
    }

    list.innerHTML = projects.map(p => `
      <div class="project-item">
        <div class="name">${p.agency_name || '-'}</div>
        <div class="sub">${p.region || ''} · ${p.status || 'pending'}</div>
      </div>
    `).join("");
  } catch (e) {
    document.getElementById("projectList").innerHTML = '<div class="loading">โหลดไม่สำเร็จ</div>';
  }
}

function showCreateProject() {
  alert("ฟีเจอร์นี้กำลังพัฒนา");
}

init();
</script>
</body>
</html>
'@
[System.IO.File]::WriteAllText("$PWD\liff\index.html", $liff)
Write-Host "[7/7] liff/index.html" -ForegroundColor Green

Write-Host ""
Write-Host "=== สร้างไฟล์ครบแล้ว! รัน go mod tidy ===" -ForegroundColor Cyan
go mod tidy

Write-Host ""
Write-Host "=== Push ขึ้น GitHub ===" -ForegroundColor Cyan
git add .
git commit -m "Add webhook, LINE login, users API, LIFF frontend"
git push

Write-Host ""
Write-Host "============================================" -ForegroundColor Green
Write-Host "DONE! Fly.io กำลัง deploy อัตโนมัติ" -ForegroundColor Green
Write-Host "URL: https://vizzel-sales-api.fly.dev" -ForegroundColor Yellow
Write-Host ""
Write-Host "ขั้นตอนต่อไป (ทำใน LINE Developers Console):" -ForegroundColor Cyan
Write-Host "1. Webhook URL: https://vizzel-sales-api.fly.dev/api/v1/webhook" -ForegroundColor White
Write-Host "2. สร้าง LIFF App -> ได้ LIFF ID -> ใส่ใน liff/index.html" -ForegroundColor White
Write-Host "============================================" -ForegroundColor Green