package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"vizzel-backend/config"
)

func GetMe(c *gin.Context) {
	lineID, _ := c.Get("line_id")

	var user struct {
		ID       string `json:"id"`
		LineID   string `json:"line_id"`
		FullName string `json:"full_name"`
		Role     string `json:"role"`
		Email    string `json:"email"`
	}

	err := config.DB.QueryRow(context.Background(),
		`SELECT id::text, line_id, COALESCE(full_name,''), COALESCE(role,''), COALESCE(email,'')
		 FROM users WHERE line_id = $1`,
		lineID,
	).Scan(&user.ID, &user.LineID, &user.FullName, &user.Role, &user.Email)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	c.JSON(http.StatusOK, user)
}
