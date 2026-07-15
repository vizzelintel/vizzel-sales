package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"vizzel-backend/config"
)

// BUG-02: documents and appointments handlers must reject dealers who do not
// belong to the project's company (IDOR). DownloadDocument already enforces
// this via userCanAccessProject; GetProjectDocuments / CreateProjectAppointment /
// DeleteProjectAppointment do not, so a dealer in Company A can read/create/
// delete another company's project data just by knowing the project id.
// See BUG_REPORT.md BUG-02.

func setupIDORFixture(t *testing.T) (attackerDealerID, victimProjectID string) {
	t.Helper()
	requireTestDB(t)
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	suffix := uniqueSuffix()

	companyA := testCompany(t, ctx, "Company A "+suffix, "AAA-"+suffix)
	companyB := testCompany(t, ctx, "Company B "+suffix, "BBB-"+suffix)
	attackerDealerID = testUser(t, ctx, "line-attacker-"+suffix, "dealer", companyA)
	victimProjectID = testProject(t, ctx, companyB, "Victim Agency "+suffix)
	return
}

func TestGetProjectDocuments_RejectsCrossCompanyDealer(t *testing.T) {
	attackerID, victimProjectID := setupIDORFixture(t)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+victimProjectID+"/documents", nil)
	c.Params = gin.Params{{Key: "id", Value: victimProjectID}}
	c.Set("user_id", attackerID)

	GetProjectDocuments(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("dealer from a different company got status %d (body %s), want 403 (BUG-02)",
			w.Code, w.Body.String())
	}
}

func TestCreateProjectAppointment_RejectsCrossCompanyDealer(t *testing.T) {
	attackerID, victimProjectID := setupIDORFixture(t)

	body, _ := json.Marshal(map[string]string{
		"scheduled_at": "2026-08-01T10:00:00Z",
	})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+victimProjectID+"/appointments/demo", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: victimProjectID}, {Key: "type", Value: "demo"}}
	c.Set("user_id", attackerID)

	CreateProjectAppointment(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("dealer from a different company created an appointment on a foreign project, "+
			"status %d (body %s), want 403 (BUG-02)", w.Code, w.Body.String())
	}
}

func TestDeleteProjectAppointment_RejectsCrossCompanyDealer(t *testing.T) {
	attackerID, victimProjectID := setupIDORFixture(t)
	ctx := context.Background()

	var apptID string
	err := config.DB.QueryRow(ctx,
		`INSERT INTO project_appointments (project_id, appt_type, scheduled_at)
		 VALUES ($1::uuid, 'demo', NOW() + interval '1 day') RETURNING id::text`,
		victimProjectID,
	).Scan(&apptID)
	if err != nil {
		t.Fatalf("insert test appointment: %v", err)
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/projects/"+victimProjectID+"/appointments/"+apptID, nil)
	c.Params = gin.Params{{Key: "id", Value: victimProjectID}, {Key: "apptId", Value: apptID}}
	c.Set("user_id", attackerID)

	DeleteProjectAppointment(c)

	if w.Code != http.StatusForbidden {
		t.Fatalf("dealer from a different company deleted a foreign project's appointment, "+
			"status %d (body %s), want 403 (BUG-02)", w.Code, w.Body.String())
	}
}
