package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"vizzel-backend/config"
	"vizzel-backend/models"
)

func GetCompanies(c *gin.Context) {
	rows, err := config.DB.Query(context.Background(),
		`SELECT id, name, COALESCE(tax_id,''), COALESCE(invite_code,''), created_at
		 FROM companies ORDER BY name ASC`,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch companies"})
		return
	}
	defer rows.Close()

	companies := make([]models.Company, 0)
	for rows.Next() {
		var co models.Company
		if err := rows.Scan(&co.ID, &co.Name, &co.TaxID, &co.InviteCode, &co.CreatedAt); err != nil {
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

	var co models.Company
	err := config.DB.QueryRow(context.Background(),
		`INSERT INTO companies (name, tax_id) VALUES ($1, NULLIF($2,''))
		 RETURNING id, name, COALESCE(tax_id,''), COALESCE(invite_code,''), created_at`,
		req.Name, req.TaxID,
	).Scan(&co.ID, &co.Name, &co.TaxID, &co.InviteCode, &co.CreatedAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create company"})
		return
	}

	c.JSON(http.StatusCreated, co)
}
