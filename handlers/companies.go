package handlers

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"vizzel-backend/config"
	"vizzel-backend/models"
)

// generateInviteCode creates a unique "<3LETTERS>-<4DIGITS>" code for a company.
func generateInviteCode(ctx context.Context, companyName string) string {
	// Extract up to 3 uppercase ASCII letters from the start of the name
	prefix := ""
	for _, ch := range strings.ToUpper(companyName) {
		if ch >= 'A' && ch <= 'Z' {
			prefix += string(ch)
			if len(prefix) == 3 {
				break
			}
		}
	}
	if len(prefix) < 3 {
		prefix = "DLR"
	}

	for i := 0; i < 10; i++ { // max 10 tries
		code := fmt.Sprintf("%s-%04d", prefix, rand.Intn(9000)+1000)
		var count int
		config.DB.QueryRow(ctx, `SELECT COUNT(*) FROM companies WHERE invite_code = $1`, code).Scan(&count)
		if count == 0 {
			return code
		}
	}
	// Fallback: prefix + timestamp suffix
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()%10000)
}

// ListAdminCompanies — admin + support: list all companies with member count.
func ListAdminCompanies(c *gin.Context) {
	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)

	var role string
	if err := config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(role,'') FROM users WHERE id = $1::uuid`, uid,
	).Scan(&role); err != nil || (role != "admin" && role != "support") {
		c.JSON(http.StatusForbidden, gin.H{"error": "ไม่มีสิทธิ์เข้าถึง"})
		return
	}

	rows, err := config.DB.Query(context.Background(), `
		SELECT c.id, c.name, COALESCE(c.type,'dealer'), COALESCE(c.invite_code,''),
		       COALESCE(c.address,''), COALESCE(c.tax_id,''), c.created_at,
		       COUNT(u.id)::int AS member_count
		FROM companies c
		LEFT JOIN users u ON u.company_id = c.id
		GROUP BY c.id, c.name, c.type, c.invite_code, c.address, c.tax_id, c.created_at
		ORDER BY c.name ASC`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch companies"})
		return
	}
	defer rows.Close()

	companies := make([]gin.H, 0)
	for rows.Next() {
		var id, name, typ, inviteCode, address, taxID string
		var createdAt time.Time
		var memberCount int
		if err := rows.Scan(&id, &name, &typ, &inviteCode, &address, &taxID, &createdAt, &memberCount); err != nil {
			continue
		}
		entry := gin.H{
			"id": id, "name": name, "type": typ,
			"address": address, "tax_id": taxID,
			"created_at": createdAt, "member_count": memberCount,
		}
		// Only admin sees invite codes
		if role == "admin" {
			entry["invite_code"] = inviteCode
		}
		companies = append(companies, entry)
	}
	c.JSON(http.StatusOK, companies)
}

// CreateAdminCompany — admin only: create a new dealer company with auto-generated invite code.
func CreateAdminCompany(c *gin.Context) {
	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)

	var role string
	if err := config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(role,'') FROM users WHERE id = $1::uuid`, uid,
	).Scan(&role); err != nil || role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin เท่านั้น"})
		return
	}

	var req models.CreateCompanyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Duplicate name check
	var existing int
	config.DB.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM companies WHERE LOWER(TRIM(name)) = LOWER(TRIM($1))`, req.Name,
	).Scan(&existing)
	if existing > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ชื่อบริษัทนี้มีอยู่แล้ว"})
		return
	}

	ctx := context.Background()
	inviteCode := generateInviteCode(ctx, req.Name)

	var co models.Company
	err := config.DB.QueryRow(ctx,
		`INSERT INTO companies (name, address, tax_id, invite_code, type)
		 VALUES ($1, NULLIF($2,''), NULLIF($3,''), $4, 'dealer')
		 RETURNING id, name, COALESCE(type,'dealer'), COALESCE(address,''), COALESCE(tax_id,''),
		           invite_code, created_at`,
		req.Name, req.Address, req.TaxID, inviteCode,
	).Scan(&co.ID, &co.Name, &co.Type, &co.Address, &co.TaxID, &co.InviteCode, &co.CreatedAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create company: " + err.Error()})
		return
	}
	c.JSON(http.StatusCreated, co)
}

// GetCompanyDetail — admin + support: return one company with members list.
// Admin sees invite_code; support does not.
func GetCompanyDetail(c *gin.Context) {
	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)
	companyID := c.Param("id")

	var role string
	if err := config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(role,'') FROM users WHERE id = $1::uuid`, uid,
	).Scan(&role); err != nil || (role != "admin" && role != "support") {
		c.JSON(http.StatusForbidden, gin.H{"error": "ไม่มีสิทธิ์เข้าถึง"})
		return
	}

	var id, name, typ, inviteCode, address, taxID string
	var createdAt time.Time
	if err := config.DB.QueryRow(context.Background(),
		`SELECT id::text, name, COALESCE(type,'dealer'), COALESCE(invite_code,''),
		        COALESCE(address,''), COALESCE(tax_id,''), created_at
		 FROM companies WHERE id = $1::uuid`, companyID,
	).Scan(&id, &name, &typ, &inviteCode, &address, &taxID, &createdAt); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "company not found"})
		return
	}

	co := gin.H{
		"id": id, "name": name, "type": typ,
		"address": address, "tax_id": taxID, "created_at": createdAt,
	}
	if role == "admin" {
		co["invite_code"] = inviteCode
	}

	// Fetch members
	rows, err := config.DB.Query(context.Background(),
		`SELECT id::text, COALESCE(full_name,''), COALESCE(first_name,''), COALESCE(last_name,''),
		        COALESCE(email,''), COALESCE(phone,''), COALESCE(region,''), COALESCE(role,''), created_at
		 FROM users WHERE company_id = $1::uuid ORDER BY created_at ASC`, companyID,
	)
	if err != nil {
		co["members"] = []gin.H{}
		c.JSON(http.StatusOK, co)
		return
	}
	defer rows.Close()

	members := make([]gin.H, 0)
	for rows.Next() {
		var mid, fullName, firstName, lastName, email, phone, region, userRole string
		var memberCreatedAt time.Time
		if err := rows.Scan(&mid, &fullName, &firstName, &lastName, &email, &phone, &region, &userRole, &memberCreatedAt); err != nil {
			continue
		}
		members = append(members, gin.H{
			"id": mid, "full_name": fullName, "first_name": firstName, "last_name": lastName,
			"email": email, "phone": phone, "region": region, "role": userRole, "created_at": memberCreatedAt,
		})
	}
	co["members"] = members
	c.JSON(http.StatusOK, co)
}

// UpdateCompanyDetail — admin only: update name, address, tax_id for any company.
func UpdateCompanyDetail(c *gin.Context) {
	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)
	companyID := c.Param("id")

	var role string
	if err := config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(role,'') FROM users WHERE id = $1::uuid`, uid,
	).Scan(&role); err != nil || role != "admin" {
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
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ชื่อบริษัทห้ามว่าง"})
		return
	}

	tag, err := config.DB.Exec(context.Background(),
		`UPDATE companies SET name=$1, address=NULLIF($2,''), tax_id=NULLIF($3,''), updated_at=NOW()
		 WHERE id=$4::uuid`,
		req.Name, req.Address, req.TaxID, companyID,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update company"})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "company not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "อัปเดตบริษัทสำเร็จ"})
}

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

func deleteCompanyMemberCore(ctx context.Context, callerID, targetID, expectedCompanyID string) error {
	if callerID == targetID {
		return fmt.Errorf("ไม่สามารถลบบัญชีของตัวเองได้")
	}
	var targetCompany string
	if err := config.DB.QueryRow(ctx,
		`SELECT COALESCE(company_id::text,'') FROM users WHERE id = $1::uuid`, targetID,
	).Scan(&targetCompany); err != nil {
		return fmt.Errorf("ไม่พบพนักงาน")
	}
	if expectedCompanyID != "" && targetCompany != expectedCompanyID {
		return fmt.Errorf("ไม่พบพนักงานในบริษัทนี้")
	}

	_, _ = config.DB.Exec(ctx, `UPDATE projects SET created_by = NULL WHERE created_by = $1::uuid`, targetID)
	_, _ = config.DB.Exec(ctx, `UPDATE project_appointments SET created_by = NULL WHERE created_by = $1::uuid`, targetID)
	_, _ = config.DB.Exec(ctx, `UPDATE documents SET uploaded_by = NULL WHERE uploaded_by = $1::uuid`, targetID)
	_, _ = config.DB.Exec(ctx, `UPDATE project_status_logs SET changed_by = NULL WHERE changed_by = $1::uuid`, targetID)

	tag, err := config.DB.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, targetID)
	if err != nil {
		return fmt.Errorf("ลบพนักงานไม่สำเร็จ: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("ไม่พบพนักงาน")
	}
	return nil
}

// DeleteCompanyMember removes a user from the caller's company (admin only, not self).
func DeleteCompanyMember(c *gin.Context) {
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
	if companyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ไม่พบบริษัทของผู้ใช้งาน"})
		return
	}
	if err := deleteCompanyMemberCore(context.Background(), uid, targetID, companyID); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "ตัวเอง") {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		} else if strings.Contains(msg, "ไม่พบ") {
			c.JSON(http.StatusNotFound, gin.H{"error": msg})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ลบพนักงานสำเร็จ"})
}

// DeleteAdminCompanyMember removes a user from any company (platform admin).
func DeleteAdminCompanyMember(c *gin.Context) {
	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)
	companyID := c.Param("id")
	targetID := c.Param("memberId")

	var role string
	if err := config.DB.QueryRow(context.Background(),
		`SELECT COALESCE(role,'') FROM users WHERE id = $1::uuid`, uid,
	).Scan(&role); err != nil || role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin เท่านั้น"})
		return
	}
	if err := deleteCompanyMemberCore(context.Background(), uid, targetID, companyID); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "ตัวเอง") {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		} else if strings.Contains(msg, "ไม่พบ") {
			c.JSON(http.StatusNotFound, gin.H{"error": msg})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ลบพนักงานสำเร็จ"})
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
