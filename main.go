package main

import (
	"log"
	"os"
	"time"

	"github.com/gin-contrib/cors"
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

	db, err := config.InitDB()
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	defer db.Close()
	log.Println("Connected to Supabase PostgreSQL")

	if os.Getenv("GIN_MODE") == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()

	r.Use(cors.New(cors.Config{
		AllowOrigins:     getAllowedOrigins(),
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-Requested-With"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "service": "vizzel-backend"})
	})

	v1 := r.Group("/api/v1")
	v1.Use(middleware.JWTAuth())
	{
		v1.POST("/projects", handlers.CreateProject(db))
		v1.GET("/projects", handlers.GetProjects(db))
		v1.POST("/documents", handlers.CreateDocument(db))
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("vizzel-backend listening on :%s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func getAllowedOrigins() []string {
	origins := []string{"http://localhost:3000", "http://localhost:5173"}
	if origin := os.Getenv("ALLOWED_ORIGINS"); origin != "" {
		origins = append(origins, origin)
	}
	return origins
}
