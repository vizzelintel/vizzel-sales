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

// BUG-06: UpdateMe's request struct uses plain strings (not pointers) and the
// SQL sets first_name/last_name unconditionally, unlike full_name which has a
// CASE WHEN guard. A partial PATCH that only sends e.g. "phone" wipes out the
// user's first_name/last_name because the omitted fields bind to "". See
// BUG_REPORT.md BUG-06.
func TestUpdateMe_PartialUpdateDoesNotClearName(t *testing.T) {
	requireTestDB(t)
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	suffix := uniqueSuffix()

	companyID := testCompany(t, ctx, "UpdateMe Test Co "+suffix, "UPD-"+suffix)
	userID := testUser(t, ctx, "line-updateme-"+suffix, "dealer", companyID)
	if _, err := config.DB.Exec(ctx,
		`UPDATE users SET first_name = 'Somchai', last_name = 'Jaidee' WHERE id = $1::uuid`, userID,
	); err != nil {
		t.Fatalf("seed name: %v", err)
	}

	// Client only wants to update phone; first_name/last_name are omitted.
	body, _ := json.Marshal(map[string]string{"phone": "0812345678"})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/me", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("user_id", userID)

	UpdateMe(c)

	if w.Code != http.StatusOK {
		t.Fatalf("UpdateMe failed: status %d body %s", w.Code, w.Body.String())
	}

	var firstName, lastName string
	if err := config.DB.QueryRow(ctx,
		`SELECT COALESCE(first_name,''), COALESCE(last_name,'') FROM users WHERE id = $1::uuid`, userID,
	).Scan(&firstName, &lastName); err != nil {
		t.Fatalf("read back user: %v", err)
	}
	if firstName != "Somchai" || lastName != "Jaidee" {
		t.Fatalf("PATCH-style update with only phone set wiped the name: first_name=%q last_name=%q, "+
			"want %q/%q preserved (BUG-06)", firstName, lastName, "Somchai", "Jaidee")
	}
}
