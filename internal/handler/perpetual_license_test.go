package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"

	"github.com/tabloy/keygate/internal/model"
	"github.com/tabloy/keygate/internal/store"
)

func setupMemoryAdminStore(t *testing.T) (*store.Store, func()) {
	t.Helper()
	ctx := context.Background()

	dsn := fmt.Sprintf("file:admin_mem_%d?mode=memory&cache=shared", time.Now().UnixNano())
	sqldb, err := sql.Open(sqliteshim.ShimName, dsn)
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}

	db := bun.NewDB(sqldb, sqlitedialect.New())

	ddl := []string{
		`CREATE TABLE IF NOT EXISTS products (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			slug TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'desktop',
			minimum_supported_version TEXT NOT NULL DEFAULT '',
			minimum_supported_message TEXT NOT NULL DEFAULT '',
			require_signing BOOLEAN NOT NULL DEFAULT 1,
			feed_license_required BOOLEAN NOT NULL DEFAULT 0,
			feed_gated_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS plans (
			id TEXT PRIMARY KEY,
			product_id TEXT NOT NULL,
			name TEXT NOT NULL,
			slug TEXT NOT NULL,
			checkout_id TEXT NOT NULL DEFAULT '',
			license_type TEXT NOT NULL,
			billing_interval TEXT DEFAULT '',
			max_activations INTEGER DEFAULT 1,
			trial_days INTEGER DEFAULT 0,
			grace_days INTEGER DEFAULT 7,
			stripe_price_id TEXT DEFAULT '',
			updates_days INTEGER DEFAULT 0,
			renewal_days INTEGER DEFAULT 0,
			stripe_renewal_price_id TEXT DEFAULT '',
			license_model TEXT DEFAULT 'standard',
			floating_timeout INTEGER DEFAULT 0,
			token_ttl_days INTEGER DEFAULT 0,
			max_seats INTEGER DEFAULT 1,
			active BOOLEAN DEFAULT 1,
			sort_order INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS entitlements (
			id TEXT PRIMARY KEY,
			plan_id TEXT NOT NULL,
			feature TEXT NOT NULL,
			value_type TEXT NOT NULL,
			value TEXT NOT NULL DEFAULT '',
			quota_period TEXT NOT NULL DEFAULT '',
			quota_unit TEXT NOT NULL DEFAULT '',
			stripe_meter_event_name TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS licenses (
			id TEXT PRIMARY KEY,
			product_id TEXT NOT NULL,
			plan_id TEXT NOT NULL,
			user_id TEXT,
			email TEXT,
			license_key TEXT,
			license_key_encrypted TEXT,
			key_hash TEXT DEFAULT '',
			payment_provider TEXT,
			stripe_customer_id TEXT,
			stripe_subscription_id TEXT,
			stripe_payment_intent_id TEXT DEFAULT '',
			stripe_checkout_session_id TEXT DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			valid_from DATETIME DEFAULT CURRENT_TIMESTAMP,
			valid_until DATETIME,
			updates_until DATETIME,
			updates_terms_set BOOLEAN NOT NULL DEFAULT 0,
			canceled_at DATETIME,
			suspended_at DATETIME,
			past_due_at DATETIME,
			notes TEXT DEFAULT '',
			org_name TEXT DEFAULT '',
			external_customer_id TEXT DEFAULT '',
			external_workspace_id TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS subscriptions (
			id TEXT PRIMARY KEY,
			license_id TEXT NOT NULL,
			user_id TEXT,
			plan_id TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			payment_provider TEXT,
			external_id TEXT,
			current_period_start DATETIME,
			current_period_end DATETIME,
			cancel_at_period_end BOOLEAN DEFAULT 0,
			canceled_at DATETIME,
			trial_start DATETIME,
			trial_end DATETIME,
			metadata TEXT DEFAULT '{}',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id TEXT PRIMARY KEY,
			entity TEXT NOT NULL,
			entity_id TEXT NOT NULL,
			action TEXT NOT NULL,
			actor_type TEXT,
			actor_id TEXT,
			ip_address TEXT,
			changes TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS activations (
			id TEXT PRIMARY KEY,
			license_id TEXT NOT NULL,
			identifier TEXT NOT NULL,
			identifier_type TEXT NOT NULL DEFAULT 'device',
			label TEXT DEFAULT '',
			ip_address TEXT DEFAULT '',
			last_verified DATETIME DEFAULT CURRENT_TIMESTAMP,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
	}

	for _, stmt := range ddl {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("exec ddl %q: %v", stmt, err)
		}
	}

	s := store.NewWithDB(db)
	cleanup := func() {
		_ = db.Close()
		_ = sqldb.Close()
	}
	return s, cleanup
}

func TestCreateLicense_PerpetualPlan_YieldsNullValidUntil(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, cleanup := setupMemoryAdminStore(t)
	defer cleanup()
	ctx := context.Background()

	h := &AdminHandler{Store: s}

	// Create test product and perpetual plan
	prod := &model.Product{
		ID:        "prod-perp-1",
		Name:      "Multi-Guard",
		Slug:      "multi-guard",
		Type:      "desktop",
		CreatedAt: time.Now(),
	}
	_, err := s.DB.NewInsert().Model(prod).Exec(ctx)
	if err != nil {
		t.Fatalf("insert product: %v", err)
	}

	perpPlan := &model.Plan{
		ID:          "plan-perp-1",
		ProductID:   prod.ID,
		Name:        "Multi-Guard AV Perpetual",
		Slug:        "mg-av-perpetual",
		LicenseType: "perpetual",
		Active:      true,
		CreatedAt:   time.Now(),
	}
	_, err = s.DB.NewInsert().Model(perpPlan).Exec(ctx)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	// 1. Calling CreateLicense with NO valid_until
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	reqBody := `{"product_id":"` + prod.ID + `","plan_id":"` + perpPlan.ID + `","email":"admin@example.com"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/licenses", strings.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	h.CreateLicense(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var resp1 struct {
		Data *model.License `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp1); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp1.Data == nil {
		t.Fatal("expected non-nil data in response")
	}
	if resp1.Data.ValidUntil != nil {
		t.Fatalf("expected valid_until to be nil for perpetual plan, got %v", resp1.Data.ValidUntil)
	}

	// Check DB row directly
	licFromDB, err := s.FindLicenseByID(ctx, resp1.Data.ID)
	if err != nil {
		t.Fatalf("FindLicenseByID: %v", err)
	}
	if licFromDB.ValidUntil != nil {
		t.Fatalf("expected DB valid_until to be NULL for perpetual license, got %v", licFromDB.ValidUntil)
	}

	// 2. Calling CreateLicense with an explicit valid_until in JSON payload on a perpetual plan
	// Backend fail-safe must ensure valid_until remains NULL!
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	futureDate := time.Now().AddDate(1, 0, 0).Format(time.RFC3339)
	reqBody2 := `{"product_id":"` + prod.ID + `","plan_id":"` + perpPlan.ID + `","email":"admin2@example.com","valid_until":"` + futureDate + `"}`
	c2.Request = httptest.NewRequest(http.MethodPost, "/admin/licenses", strings.NewReader(reqBody2))
	c2.Request.Header.Set("Content-Type", "application/json")

	h.CreateLicense(c2)

	if w2.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w2.Code, w2.Body.String())
	}

	var resp2 struct {
		Data *model.License `json:"data"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("unmarshal response 2: %v", err)
	}
	if resp2.Data.ValidUntil != nil {
		t.Fatalf("FAIL-SAFE VIOLATION: perpetual plan got non-null valid_until: %v", resp2.Data.ValidUntil)
	}

	lic2FromDB, err := s.FindLicenseByID(ctx, resp2.Data.ID)
	if err != nil {
		t.Fatalf("FindLicenseByID 2: %v", err)
	}
	if lic2FromDB.ValidUntil != nil {
		t.Fatalf("FAIL-SAFE VIOLATION: DB row has non-null valid_until for perpetual license: %v", lic2FromDB.ValidUntil)
	}
}

func TestCreateLicense_SubscriptionPlan_AllowsValidUntil(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, cleanup := setupMemoryAdminStore(t)
	defer cleanup()
	ctx := context.Background()

	h := &AdminHandler{Store: s}

	prod := &model.Product{
		ID:        "prod-sub-1",
		Name:      "Multi-Guard",
		Slug:      "multi-guard",
		Type:      "desktop",
		CreatedAt: time.Now(),
	}
	_, _ = s.DB.NewInsert().Model(prod).Exec(ctx)

	subPlan := &model.Plan{
		ID:              "plan-sub-1",
		ProductID:       prod.ID,
		Name:            "Multi-Guard Monthly",
		Slug:            "mg-monthly",
		LicenseType:     "subscription",
		BillingInterval: "month",
		Active:          true,
		CreatedAt:       time.Now(),
	}
	_, _ = s.DB.NewInsert().Model(subPlan).Exec(ctx)

	future := time.Now().AddDate(0, 1, 0).Truncate(time.Second)
	futureRFC := future.Format(time.RFC3339)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	reqBody := `{"product_id":"` + prod.ID + `","plan_id":"` + subPlan.ID + `","email":"sub@example.com","valid_until":"` + futureRFC + `"}`
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/licenses", strings.NewReader(reqBody))
	c.Request.Header.Set("Content-Type", "application/json")

	h.CreateLicense(c)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data *model.License `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Data.ValidUntil == nil {
		t.Fatal("expected valid_until to be set for subscription plan")
	}

	licFromDB, err := s.FindLicenseByID(ctx, resp.Data.ID)
	if err != nil {
		t.Fatalf("FindLicenseByID: %v", err)
	}
	if licFromDB.ValidUntil == nil {
		t.Fatal("expected DB valid_until to be set")
	}
}

func TestSetLicenseValidUntil_ClearingDateMakesLicensePerpetual(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s, cleanup := setupMemoryAdminStore(t)
	defer cleanup()
	ctx := context.Background()

	h := &AdminHandler{Store: s}

	prod := &model.Product{
		ID:        "prod-edit-1",
		Name:      "Multi-Guard",
		Slug:      "multi-guard",
		Type:      "desktop",
		CreatedAt: time.Now(),
	}
	_, _ = s.DB.NewInsert().Model(prod).Exec(ctx)

	plan := &model.Plan{
		ID:          "plan-edit-1",
		ProductID:   prod.ID,
		Name:        "Multi-Guard Standard",
		Slug:        "mg-std",
		LicenseType: "subscription",
		Active:      true,
		CreatedAt:   time.Now(),
	}
	_, _ = s.DB.NewInsert().Model(plan).Exec(ctx)

	// Create license with an initial expiry date
	expiry := time.Now().AddDate(0, 6, 0)
	lic := &model.License{
		ID:         "lic-to-clear",
		ProductID:  prod.ID,
		PlanID:     plan.ID,
		Email:      "user@example.com",
		LicenseKey: "TEST-KEY-1234",
		Status:     model.StatusActive,
		ValidUntil: &expiry,
	}
	if err := s.CreateLicense(ctx, lic); err != nil {
		t.Fatalf("create license: %v", err)
	}

	// Verify it has an expiration date initially
	check1, err := s.FindLicenseByID(ctx, lic.ID)
	if err != nil || check1.ValidUntil == nil {
		t.Fatalf("expected initial valid_until to be set, got err=%v, val=%v", err, check1.ValidUntil)
	}

	// Send POST /admin/licenses/:id/valid-until with empty string ""
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: lic.ID}}
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/licenses/"+lic.ID+"/valid-until", strings.NewReader(`{"valid_until":""}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.SetLicenseValidUntil(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var updated model.License
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if updated.ValidUntil != nil {
		t.Fatalf("expected response valid_until to be nil, got %v", updated.ValidUntil)
	}

	// Confirm directly in database
	dbLic, err := s.FindLicenseByID(ctx, lic.ID)
	if err != nil {
		t.Fatalf("FindLicenseByID: %v", err)
	}
	if dbLic.ValidUntil != nil {
		t.Fatalf("expected database valid_until to be NULL after clear, got %v", dbLic.ValidUntil)
	}
}
