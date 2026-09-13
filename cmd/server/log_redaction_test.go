package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// An updater that cannot set headers sends its credential as a query
// parameter. It must not survive into a log line.
func TestRedactPath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/api/v1/feeds/app/sparkle.xml", "/api/v1/feeds/app/sparkle.xml"},
		{"/f?platform=darwin", "/f?platform=darwin"},
		{"/f?license_key=KG-SECRET-1234", "/f?license_key=redacted"},
		{"/f?platform=darwin&license_key=KG-SECRET&channel=beta", "/f?channel=beta&license_key=redacted&platform=darwin"},
		{"/f?license_token=abc.def", "/f?license_token=redacted"},
		{"/f?license_key=a&license_key=b", "/f?license_key=redacted"},
		{"/f?%zz", "/f?<redacted>"},
	}
	for _, c := range cases {
		if got := redactPath(c.in); got != c.want {
			t.Errorf("redactPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A panic must not spill the credential the request carried. Gin's
// own recovery dumps the request with only Authorization masked.
func TestRedactedRecoveryLogsNoCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	r := gin.New()
	r.Use(redactedRecovery())
	r.GET("/boom", func(c *gin.Context) { panic("kaboom") })
	req := httptest.NewRequest(http.MethodGet, "/boom?license_key=KGT-SECRET-VALUE", nil)
	req.Header.Set("X-License-Key", "KGT-HEADER-SECRET")
	req.Header.Set("X-License-Token", "tok.secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: %d want 500", w.Code)
	}
	logged := buf.String()
	for _, secret := range []string{"KGT-SECRET-VALUE", "KGT-HEADER-SECRET", "tok.secret"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("the log kept %q: %s", secret, logged)
		}
	}
	if !strings.Contains(logged, "kaboom") || !strings.Contains(logged, "/boom") {
		t.Fatalf("the log says too little: %s", logged)
	}
}
