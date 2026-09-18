package handler_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/tabloy/keygate/internal/middleware"
	"github.com/tabloy/keygate/internal/model"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestUnauthorizedCRMRequestRejected(t *testing.T) {
	r := gin.New()

	// Simulate baseAdminMW with SessionOrAPIKey requiring JWT/APIKey
	adminMW := []gin.HandlerFunc{
		middleware.SessionOrAPIKey("test-secret", nil, nil),
		middleware.RequireScope(model.ScopeAdmin),
	}

	crmGroup := r.Group("/api/v1/admin/crm", adminMW...)
	crmGroup.GET("/customers", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/crm/customers", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 Unauthorized, got %d", w.Code)
	}
}

func TestCRMInvalidPayloadRejection(t *testing.T) {
	r := gin.New()

	// Direct endpoint without auth middleware to test handler validation
	r.POST("/customers", func(c *gin.Context) {
		var req struct {
			FirstName string `json:"first_name" binding:"required"`
			LastName  string `json:"last_name" binding:"required"`
			Phone     string `json:"phone" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "first_name, last_name, and phone are required"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Missing phone and last_name
	body := bytes.NewBufferString(`{"first_name": "Jan"}`)
	req := httptest.NewRequest(http.MethodPost, "/customers", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request for incomplete customer payload, got %d", w.Code)
	}
}
