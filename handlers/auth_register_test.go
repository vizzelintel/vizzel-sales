package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"vizzel-backend/config"
)

var errLineProfileFixture = errors.New("invalid access token (test fixture)")

func registerRequestBody(accessToken, inviteCode string) *http.Request {
	body, _ := json.Marshal(RegisterRequest{
		AccessToken: accessToken,
		FirstName:   "Test",
		LastName:    "User",
		InviteCode:  inviteCode,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// stubLineProfile swaps fetchLineProfile for the duration of the test so
// Register can be exercised without calling the real LINE API.
func stubLineProfile(t *testing.T, profile *LineProfile, err error) {
	t.Helper()
	prev := fetchLineProfile
	fetchLineProfile = func(accessToken string) (*LineProfile, error) { return profile, err }
	t.Cleanup(func() { fetchLineProfile = prev })
}

// BUG-03 (fixed): Register now verifies LINE ownership via
// fetchLineProfile(access_token), the same as LineLogin, and always derives
// the line_id from that verified profile rather than trusting client input.

func TestRegister_RejectsMissingAccessToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body, _ := json.Marshal(map[string]string{
		"first_name":  "Test",
		"last_name":   "User",
		"invite_code": "SOME-CODE",
	})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	Register(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Register without access_token = %d, want 400 (BUG-03 fix: access_token is required)", w.Code)
	}
}

func TestRegister_RejectsInvalidLineToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stubLineProfile(t, nil, errLineProfileFixture)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = registerRequestBody("bad-token", "SOME-CODE")

	Register(c)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Register with an invalid LINE access token = %d, want 401 (BUG-03 fix)", w.Code)
	}
}

func TestRegister_UsesVerifiedLineIDFromProfile(t *testing.T) {
	requireTestDB(t)
	gin.SetMode(gin.TestMode)
	os.Setenv("JWT_SECRET", "test-jwt-secret")
	defer os.Unsetenv("JWT_SECRET")
	ctx := context.Background()
	suffix := uniqueSuffix()

	inviteCode := "REG-" + suffix
	testCompany(t, ctx, "Register Test Co "+suffix, inviteCode)

	verifiedLineID := "verified-line-id-" + suffix
	stubLineProfile(t, &LineProfile{UserID: verifiedLineID, DisplayName: "Real Owner"}, nil)
	t.Cleanup(func() {
		config.DB.Exec(context.Background(), `DELETE FROM users WHERE line_id = $1`, verifiedLineID)
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = registerRequestBody("whatever-token", inviteCode)

	Register(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("Register with a valid LINE token failed: status %d body %s", w.Code, w.Body.String())
	}

	var count int
	if err := config.DB.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE line_id = $1`, verifiedLineID).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected a user row created with the verified line_id %q, found %d", verifiedLineID, count)
	}
}

// BUG-08: two concurrent Register calls for the same not-yet-registered
// line_id race between the duplicate-check SELECT and the INSERT. The DB's
// UNIQUE(line_id) constraint stops a duplicate row, but the losing request's
// INSERT error isn't handled, so it currently surfaces as a 500 instead of a
// graceful "already registered" response. See BUG_REPORT.md BUG-08.
func TestRegister_ConcurrentSameLineID_NoServerError(t *testing.T) {
	requireTestDB(t)
	gin.SetMode(gin.TestMode)
	os.Setenv("JWT_SECRET", "test-jwt-secret")
	defer os.Unsetenv("JWT_SECRET")
	ctx := context.Background()
	suffix := uniqueSuffix()

	inviteCode := "RACE-" + suffix
	testCompany(t, ctx, "Race Test Co "+suffix, inviteCode)
	lineID := "race-line-id-" + suffix
	stubLineProfile(t, &LineProfile{UserID: lineID, DisplayName: "Race Tester"}, nil)
	t.Cleanup(func() {
		config.DB.Exec(context.Background(), `DELETE FROM users WHERE line_id = $1`, lineID)
	})

	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = registerRequestBody("whatever-token", inviteCode)
			Register(c)
			codes[i] = w.Code
		}(i)
	}
	wg.Wait()

	for i, code := range codes {
		if code == http.StatusInternalServerError {
			t.Errorf("concurrent registration %d for the same line_id returned 500 instead of a "+
				"graceful duplicate response — Register should use ON CONFLICT / re-check instead of "+
				"relying on an unhandled unique-constraint error (BUG-08); codes=%v", i, codes)
		}
	}

	var count int
	if err := config.DB.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE line_id = $1`, lineID).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 user row for line_id %q after concurrent registration, got %d", lineID, count)
	}
}
