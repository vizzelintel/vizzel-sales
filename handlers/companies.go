package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"vizzel-backend/config"
	"vizzel-backend/models"
)

func GetCompanies(c *gin.Context) {
	rows, err := config.DB.Query(context.Background(),
		`SELECT id, name, domain, logo_url, created_at, updated_at FROM companies ORDER BY name ASC`,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch companies"})
		return
	}
	defer rows.Close()

	companies := make([]models.Company, 0)
	for rows.Next() {
		var co models.Company
		if err := rows.Scan(&co.ID, &co.Name, &co.Domain, &co.LogoURL, &co.CreatedAt, &co.UpdatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse companies"})
			return
		}
		companies = append(companies, co)
	}

	c.JSON(http.StatusOK, companies)
}

func CreateCompany(c *gin.Context) {
	var req models.CreateCompanyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	now := time.Now().UTC()
	co := models.Company{
		Name:      req.Name,
		Domain:    req.Domain,
		CreatedAt: now,
		UpdatedAt: now,
	}

	err := config.DB.QueryRow(context.Background(),
		`INSERT INTO companies (name, domain, created_at, updated_at) VALUES ($1, $2, $3, $4) RETURNING id`,
		co.Name, co.Domain, co.CreatedAt, co.UpdatedAt,
	).Scan(&co.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create company"})
		return
	}

	c.JSON(http.StatusCreated, co)
}
