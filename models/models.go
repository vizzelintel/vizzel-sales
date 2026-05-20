package models

import "time"

type Company struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	TaxID      string    `json:"tax_id,omitempty"`
	InviteCode string    `json:"invite_code,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type User struct {
	ID        string    `json:"id"`
	LineID    string    `json:"line_id"`
	CompanyID string    `json:"company_id,omitempty"`
	FullName  string    `json:"full_name"`
	Email     string    `json:"email,omitempty"`
	Phone     string    `json:"phone,omitempty"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type Project struct {
	ID              string    `json:"id"`
	CompanyID       string    `json:"company_id,omitempty"`
	AgencyName      string    `json:"agency_name"`
	AgencyType      string    `json:"agency_type,omitempty"`
	Region          string    `json:"region,omitempty"`
	ContactPerson   string    `json:"contact_person,omitempty"`
	ContactPosition string    `json:"contact_position,omitempty"`
	ContactPhone    string    `json:"contact_phone,omitempty"`
	Status          string    `json:"status"`
	StatusNote      string    `json:"status_note,omitempty"`
	RejectReason    string    `json:"reject_reason,omitempty"`
	CreatedBy       string    `json:"created_by,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	AppointmentDate string    `json:"appointment_date,omitempty"`
	AppointmentNote string    `json:"appointment_note,omitempty"`
	CalendarEventID string    `json:"calendar_event_id,omitempty"`
}

type Document struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id"`
	DocType    string    `json:"doc_type"`
	FileURL    string    `json:"file_url"`
	UploadedBy string    `json:"uploaded_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type CreateProjectRequest struct {
	AgencyName      string `json:"agency_name" binding:"required"`
	AgencyType      string `json:"agency_type"`
	Region          string `json:"region"`
	ContactPerson   string `json:"contact_person"`
	ContactPosition string `json:"contact_position"`
	ContactPhone    string `json:"contact_phone"`
	CompanyID       string `json:"company_id"`
}

type UpdateProjectStatusRequest struct {
	Status          string `json:"status" binding:"required"`
	StatusNote      string `json:"status_note"`
	RejectReason    string `json:"reject_reason"`
	AppointmentDate string `json:"appointment_date"` // RFC3339; only for present/demo/site_survey
	AppointmentNote string `json:"appointment_note"`
}

type CreateCompanyRequest struct {
	Name  string `json:"name" binding:"required"`
	TaxID string `json:"tax_id"`
}
