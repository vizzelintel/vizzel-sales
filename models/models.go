package models

import "time"

// Company matches the actual DB schema (no domain/logo_url/updated_at).
type Company struct {
	ID         string    `json:"id" db:"id"`
	Name       string    `json:"name" db:"name"`
	TaxID      string    `json:"tax_id,omitempty" db:"tax_id"`
	InviteCode string    `json:"invite_code,omitempty" db:"invite_code"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

type User struct {
	ID        string    `json:"id" db:"id"`
	LineID    string    `json:"line_id" db:"line_id"`
	CompanyID string    `json:"company_id,omitempty" db:"company_id"`
	FullName  string    `json:"full_name" db:"full_name"`
	Email     string    `json:"email,omitempty" db:"email"`
	Phone     string    `json:"phone,omitempty" db:"phone"`
	Role      string    `json:"role" db:"role"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// Project matches the actual DB schema (no updated_at; created_by is a UUID).
type Project struct {
	ID            string    `json:"id" db:"id"`
	CompanyID     string    `json:"company_id,omitempty" db:"company_id"`
	AgencyName    string    `json:"agency_name" db:"agency_name"`
	Region        string    `json:"region,omitempty" db:"region"`
	ContactPerson string    `json:"contact_person,omitempty" db:"contact_person"`
	ContactPhone  string    `json:"contact_phone,omitempty" db:"contact_phone"`
	Status        string    `json:"status" db:"status"`
	CreatedBy     string    `json:"created_by,omitempty" db:"created_by"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
}

type Document struct {
	ID        string    `json:"id" db:"id"`
	ProjectID string    `json:"project_id" db:"project_id"`
	Name      string    `json:"name" db:"name"`
	Type      string    `json:"type" db:"type"`
	URL       string    `json:"url,omitempty" db:"url"`
	Size      int64     `json:"size" db:"size"`
	CreatedBy string    `json:"created_by" db:"created_by"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

type CreateProjectRequest struct {
	AgencyName    string `json:"agency_name" binding:"required"`
	Region        string `json:"region"`
	ContactPerson string `json:"contact_person"`
	ContactPhone  string `json:"contact_phone"`
	Status        string `json:"status"`
	CompanyID     string `json:"company_id"`
}

type UpdateProjectStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

type CreateCompanyRequest struct {
	Name  string `json:"name" binding:"required"`
	TaxID string `json:"tax_id"`
}

type CreateDocumentRequest struct {
	ProjectID string `json:"project_id" binding:"required"`
	Name      string `json:"name" binding:"required"`
	Type      string `json:"type" binding:"required"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
}
