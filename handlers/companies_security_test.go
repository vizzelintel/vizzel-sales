package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"vizzel-backend/models"
)

// BUG-01: GetCompanies (GET /api/v1/companies) must not leak invite_code to
// non-admin callers. Any authenticated dealer can currently read every
// company's invite_code, which is the secret used by /register to join a
// company. See BUG_REPORT.md BUG-01.
func TestGetCompanies_DoesNotLeakInviteCodeToDealer(t *testing.T) {
	requireTestDB(t)
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	suffix := uniqueSuffix()
	companyID := testCompany(t, ctx, "Victim Co "+suffix, "SECRET-"+suffix)
	dealerID := testUser(t, ctx, "line-dealer-"+suffix, "dealer", companyID)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/companies", nil)
	c.Set("user_id", dealerID)

	GetCompanies(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var companies []models.Company
	if err := json.Unmarshal(w.Body.Bytes(), &companies); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, co := range companies {
		if co.InviteCode != "" {
			t.Fatalf("dealer received invite_code %q for company %q — GetCompanies must hide "+
				"invite_code from non-admin callers (BUG-01)", co.InviteCode, co.Name)
		}
	}
}
