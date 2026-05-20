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
	r.MaxMultipartMemory = 8 << 20 // 8 MB max for file uploads

	allowedOrigins := map[string]bool{
		"https://vizzelintel.github.io":                    true,
		"https://vizzel-sales-production.up.railway.app": true,
	}
	r.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if allowedOrigins[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
		} else {
			c.Header("Access-Control-Allow-Origin", "*")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"service": "vizzel-backend", "status": "ok"})
	})
	r.POST("/api/v1/webhook", handlers.HandleWebhook)
	r.POST("/api/v1/auth/line", handlers.LineLogin)

	api := r.Group("/api/v1")
	api.Use(middleware.JWTAuth())
	{
		api.GET("/users/me", handlers.GetMe) // legacy path kept
		api.GET("/me", handlers.GetMe)
		api.PUT("/me", handlers.UpdateMe)

		api.GET("/company", handlers.GetCompany)
		api.PUT("/company", handlers.UpdateCompany)
		api.GET("/company/members", handlers.GetCompanyMembers)
		api.PUT("/company/members/:id/role", handlers.UpdateMemberRole)

		api.GET("/projects", handlers.GetProjects)
		api.POST("/projects", handlers.CreateProject)
		api.GET("/projects/:id", handlers.GetProject)
		api.PUT("/projects/:id", handlers.UpdateProject)
		api.PUT("/projects/:id/status", handlers.UpdateProjectStatus)
		api.GET("/projects/:id/documents", handlers.GetProjectDocuments)

		api.GET("/companies", handlers.GetCompanies)
		api.POST("/companies", handlers.CreateCompany)

		api.POST("/documents", handlers.CreateDocument)
		api.DELETE("/documents/:id", handlers.DeleteDocument)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("vizzel-backend listening on :%s", port)
	r.Run(":" + port)
}
