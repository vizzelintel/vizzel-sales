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

// GetCompany returns the authenticated user's company.
// Dealers get 403; support sees basic info; admin sees invite_code too.
func GetCompany(c *gin.Context) {
	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)

	var role, companyID string
	if err := config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(role,''), COALESCE(company_id::text,'') FROM users WHERE id = $1::uuid`, uid,
	).Scan(&role, &companyID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if role == "dealer" || role == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "ไม่มีสิทธิ์เข้าถึง"})
		return
	}
	if companyID == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "ไม่พบข้อมูลบริษัท"})
		return
	}

	var co models.Company
	var address string
	if err := config.DB.QueryRow(context.Background(),
		`SELECT id, name, COALESCE(address,''), COALESCE(tax_id,''), COALESCE(invite_code,''), created_at
		 FROM companies WHERE id = $1::uuid`, companyID,
	).Scan(&co.ID, &co.Name, &address, &co.TaxID, &co.InviteCode, &co.CreatedAt); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "company not found"})
		return
	}
	co.Address = address

	resp := gin.H{
		"id": co.ID, "name": co.Name, "address": co.Address,
		"tax_id": co.TaxID, "created_at": co.CreatedAt,
	}
	if role == "admin" {
		resp["invite_code"] = co.InviteCode
	}
	c.JSON(http.StatusOK, resp)
}

// UpdateCompany — admin only.
func UpdateCompany(c *gin.Context) {
	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)

	var role, companyID string
	if err := config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(role,''), COALESCE(company_id::text,'') FROM users WHERE id = $1::uuid`, uid,
	).Scan(&role, &companyID); err != nil || role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin เท่านั้น"})
		return
	}

	var req struct {
		Name    string `json:"name"`
		Address string `json:"address"`
		TaxID   string `json:"tax_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if _, err := config.DB.Exec(context.Background(),
		`UPDATE companies SET name=$1, address=$2, tax_id=$3, updated_at=NOW() WHERE id=$4::uuid`,
		req.Name, req.Address, req.TaxID, companyID,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update company"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "อัปเดตบริษัทสำเร็จ"})
}

// GetCompanyMembers — support and admin only.
func GetCompanyMembers(c *gin.Context) {
	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)

	var role, companyID string
	if err := config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(role,''), COALESCE(company_id::text,'') FROM users WHERE id = $1::uuid`, uid,
	).Scan(&role, &companyID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if role == "dealer" || role == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "ไม่มีสิทธิ์เข้าถึง"})
		return
	}
	if companyID == "" {
		c.JSON(http.StatusOK, []gin.H{})
		return
	}

	rows, err := config.DB.Query(context.Background(),
		`SELECT id::text, COALESCE(full_name,''), COALESCE(first_name,''), COALESCE(last_name,''),
		        COALESCE(email,''), COALESCE(phone,''), COALESCE(region,''), COALESCE(role,''), created_at
		 FROM users WHERE company_id = $1::uuid ORDER BY created_at ASC`, companyID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch members"})
		return
	}
	defer rows.Close()

	members := make([]gin.H, 0)
	for rows.Next() {
		var id, fullName, firstName, lastName, email, phone, region, userRole string
		var createdAt time.Time
		if err := rows.Scan(&id, &fullName, &firstName, &lastName, &email, &phone, &region, &userRole, &createdAt); err != nil {
			continue
		}
		members = append(members, gin.H{
			"id": id, "full_name": fullName, "first_name": firstName, "last_name": lastName,
			"email": email, "phone": phone, "region": region, "role": userRole, "created_at": createdAt,
		})
	}
	c.JSON(http.StatusOK, members)
}

// UpdateMemberRole — admin only, cannot change own role.
func UpdateMemberRole(c *gin.Context) {
	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)
	targetID := c.Param("id")

	var role, companyID string
	if err := config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(role,''), COALESCE(company_id::text,'') FROM users WHERE id = $1::uuid`, uid,
	).Scan(&role, &companyID); err != nil || role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin เท่านั้น"})
		return
	}
	if uid == targetID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ไม่สามารถเปลี่ยน role ของตัวเองได้"})
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Role != "dealer" && req.Role != "support" && req.Role != "admin" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role ไม่ถูกต้อง (dealer/support/admin)"})
		return
	}

	if _, err := config.DB.Exec(context.Background(),
		`UPDATE users SET role=$1 WHERE id=$2::uuid AND company_id=$3::uuid`,
		req.Role, targetID, companyID,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update role"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "อัปเดต role สำเร็จ"})
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
