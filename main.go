package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

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
	handlers.InitDocumentStorage()

	var r *gin.Engine
	if os.Getenv("GIN_MODE") == "release" {
		gin.SetMode(gin.ReleaseMode)
		r = gin.New()
		r.Use(gin.Recovery())
	} else {
		r = gin.Default()
	}
	r.MaxMultipartMemory = 8 << 20 // 8 MB max for file uploads

	allowedOrigins := config.AllowedCORSOrigins()
	r.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin != "" && allowedOrigins[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	r.GET("/health", healthHandler)
	r.HEAD("/health", healthHandler)
	r.POST("/api/v1/webhook", handlers.HandleWebhook)
	r.POST("/api/v1/webhook/lark", handlers.HandleLarkWebhook)
	r.POST("/api/v1/auth/line", handlers.LineLogin)
	r.POST("/api/v1/auth/register", handlers.Register)
	r.GET("/api/v1/auth/validate-invite", handlers.ValidateInviteCode)

	api := r.Group("/api/v1")
	api.Use(middleware.JWTAuth())
	{
		api.POST("/auth/email/send-otp", handlers.SendEmailOTP)
		api.POST("/auth/email/verify-otp", handlers.VerifyEmailOTP)
		api.Use(middleware.EmailVerifiedGuard())

		api.GET("/users/me", handlers.GetMe)
		api.GET("/me", handlers.GetMe)
		api.PUT("/me", handlers.UpdateMe)

		api.GET("/company", handlers.GetCompany)
		api.PUT("/company", handlers.UpdateCompany)
		api.GET("/company/members", handlers.GetCompanyMembers)
		api.PUT("/company/members/:id/role", handlers.UpdateMemberRole)
		api.DELETE("/company/members/:id", handlers.DeleteCompanyMember)
		api.DELETE("/admin/companies/:id/members/:memberId", handlers.DeleteAdminCompanyMember)
		api.DELETE("/admin/companies/:id", handlers.DeleteAdminCompany)

		api.GET("/projects", handlers.GetProjects)
		api.POST("/projects", handlers.CreateProject)
		api.GET("/projects/:id", handlers.GetProject)
		api.PUT("/projects/:id", handlers.UpdateProject)
		api.PUT("/projects/:id/status", handlers.UpdateProjectStatus)
		api.GET("/projects/:id/appointments", handlers.GetProjectAppointments)
		api.POST("/projects/:id/appointments/:type", handlers.CreateProjectAppointment)
		api.PUT("/projects/:id/appointments/:type", handlers.UpsertProjectAppointment)
		api.PATCH("/projects/:id/appointments/:apptId", handlers.UpdateProjectAppointment)
		api.DELETE("/projects/:id/appointments/:apptId", handlers.DeleteProjectAppointment)
		api.GET("/projects/:id/documents", handlers.GetProjectDocuments)

		api.GET("/companies", handlers.GetCompanies)
		api.POST("/companies", handlers.CreateCompany)

		api.GET("/admin/companies", handlers.ListAdminCompanies)
		api.POST("/admin/companies", handlers.CreateAdminCompany)
		api.GET("/admin/companies/:id", handlers.GetCompanyDetail)
		api.PUT("/admin/companies/:id", handlers.UpdateCompanyDetail)
		api.POST("/admin/sync-lark", handlers.SyncAllProjectsToLark)
		api.GET("/admin/lark-diagnose", handlers.LarkDiagnose)
		api.POST("/admin/lark-sync-probe", handlers.LarkSyncProbe)
		api.POST("/admin/lark-pull-inbound", handlers.LarkPullInbound)

		api.POST("/documents", handlers.CreateDocument)
		api.GET("/documents/:id/download", handlers.DownloadDocument)
		api.DELETE("/documents/:id", handlers.DeleteDocument)
	}

	go handlers.StartAutoRejectCron()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("vizzel-backend listening on :%s", port)
	r.Run(":" + port)
}

func healthHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	dbStatus := "ok"
	if err := config.PingDB(ctx); err != nil {
		dbStatus = "error"
	}

	status := "ok"
	code := http.StatusOK
	if dbStatus != "ok" {
		status = "degraded"
		code = http.StatusServiceUnavailable
	}

	if c.Request.Method == http.MethodHead {
		c.Status(code)
		return
	}

	c.JSON(code, gin.H{
		"service": "vizzel-backend",
		"status":  status,
		"db":      dbStatus,
	})
}
