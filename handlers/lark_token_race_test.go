package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
)

// BUG-04: larkCachedToken / larkTokenExpiry are package-level vars read and
// written by getLarkAccessToken with no mutex, while callers invoke it from
// many goroutines concurrently (SyncProjectToLark, webhook handling, cron).
// Run with `go test -race ./handlers/... -run TestGetLarkAccessToken_ConcurrentAccess`
// to observe the race directly; this test also asserts every call gets a
// valid token, which can be corrupted under a torn read/write without a lock.
// See BUG_REPORT.md BUG-04.
func TestGetLarkAccessToken_ConcurrentAccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":                0,
			"msg":                 "ok",
			"tenant_access_token": "tok-race-test",
			"expire":              7200,
		})
	}))
	defer srv.Close()

	os.Setenv("LARK_API_BASE", srv.URL)
	os.Setenv("LARK_APP_ID", "app-id")
	os.Setenv("LARK_APP_SECRET", "app-secret")
	defer os.Unsetenv("LARK_API_BASE")
	defer os.Unsetenv("LARK_APP_ID")
	defer os.Unsetenv("LARK_APP_SECRET")

	// Reset shared cache state so this test is order-independent.
	larkCachedToken = ""
	larkTokenExpiry = larkTokenExpiry.AddDate(-1, 0, 0)

	const goroutines = 50
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := getLarkAccessToken()
			if err != nil {
				errs <- err
				return
			}
			if tok != "tok-race-test" {
				errs <- errUnexpectedToken(tok)
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent getLarkAccessToken() call failed: %v (BUG-04: unsynchronized cache access)", err)
	}
}

type errUnexpectedToken string

func (e errUnexpectedToken) Error() string {
	return "unexpected token value: " + string(e)
}
