package models

import "time"

type Company struct {
	ID        string    `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	Domain    string    `json:"domain,omitempty" db:"domain"`
	LogoURL   string    `json:"logo_url,omitempty" db:"logo_url"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

type User struct {
	ID        string    `json:"id" db:"id"`
	LineID    string    `json:"line_id" db:"line_id"`
	CompanyID string    `json:"company_id,omitempty" db:"company_id"`
	Name      string    `json:"name" db:"name"`
	Email     string    `json:"email,omitempty" db:"email"`
	Picture   string    `json:"picture,omitempty" db:"picture"`
	Role      string    `json:"role" db:"role"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

type Project struct {
	ID            string    `json:"id" db:"id"`
	CompanyID     string    `json:"company_id,omitempty" db:"company_id"`
	AgencyName    string    `json:"agency_name" db:"agency_name"`
	Region        string    `json:"region,omitempty" db:"region"`
	ContactPerson string    `json:"contact_person,omitempty" db:"contact_person"`
	ContactPhone  string    `json:"contact_phone,omitempty" db:"contact_phone"`
	Status        string    `json:"status" db:"status"`
	CreatedBy     string    `json:"created_by" db:"created_by"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
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
	Name   string `json:"name" binding:"required"`
	Domain string `json:"domain"`
}

type CreateDocumentRequest struct {
	ProjectID string `json:"project_id" binding:"required"`
	Name      string `json:"name" binding:"required"`
	Type      string `json:"type" binding:"required"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
}
