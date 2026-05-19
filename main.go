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

r.GET("/health", func(c *gin.Context) {
c.JSON(200, gin.H{"service": "vizzel-backend", "status": "ok"})
})
r.POST("/api/v1/webhook", handlers.HandleWebhook)
r.POST("/api/v1/auth/line", handlers.LineLogin)

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
