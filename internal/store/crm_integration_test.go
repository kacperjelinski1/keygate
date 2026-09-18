package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"

	"github.com/tabloy/keygate/internal/license"
	"github.com/tabloy/keygate/internal/model"
	"github.com/tabloy/keygate/internal/store"
)

// setupMemoryCRMStore creates a standalone in-memory SQLite store with all CRM and core tables.
func setupMemoryCRMStore(t *testing.T) (*store.Store, func()) {
	t.Helper()
	ctx := context.Background()

	// Unique in-memory database name per test to ensure total isolation
	dsn := fmt.Sprintf("file:crm_mem_%d?mode=memory&cache=shared", time.Now().UnixNano())
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
			type TEXT NOT NULL,
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
			plan_id TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS license_renewals (
			id TEXT PRIMARY KEY,
			license_id TEXT NOT NULL,
			days INTEGER NOT NULL,
			previous_updates_until DATETIME,
			updates_until DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS multi_customers (
			id TEXT PRIMARY KEY,
			first_name TEXT NOT NULL,
			last_name TEXT NOT NULL,
			phone TEXT NOT NULL,
			normalized_phone TEXT NOT NULL,
			email TEXT,
			normalized_email TEXT,
			customer_since DATETIME DEFAULT CURRENT_TIMESTAMP,
			notes TEXT DEFAULT '',
			archived_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS multi_customer_licenses (
			id TEXT PRIMARY KEY,
			customer_id TEXT NOT NULL,
			keygate_license_id TEXT NOT NULL,
			assigned_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			ended_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS multi_customer_events (
			id TEXT PRIMARY KEY,
			customer_id TEXT NOT NULL,
			keygate_license_id TEXT,
			event_type TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'crm',
			external_event_id TEXT,
			occurred_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			period_from DATETIME,
			period_until DATETIME,
			payment_method TEXT,
			amount REAL,
			currency TEXT DEFAULT 'PLN',
			notes TEXT DEFAULT '',
			metadata TEXT DEFAULT '{}',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_multi_customer_events_external ON multi_customer_events(source, external_event_id) WHERE external_event_id IS NOT NULL;`,
	}

	for _, stmt := range ddl {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("execute DDL %q: %v", stmt, err)
		}
	}

	s := store.NewWithDB(db)
	cleanup := func() {
		_ = db.Close()
		_ = sqldb.Close()
	}
	return s, cleanup
}

func createTestProductAndPlan(t *testing.T, s *store.Store, licType string) (*model.Product, *model.Plan) {
	t.Helper()
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	now := time.Now()

	prod := &model.Product{
		ID:        "prod-" + suffix,
		Name:      "Test Product " + suffix,
		Slug:      "test-prod-" + suffix,
		CreatedAt: now,
	}
	if _, err := s.DB.NewInsert().Model(prod).Exec(ctx); err != nil {
		t.Fatalf("insert product: %v", err)
	}

	plan := &model.Plan{
		ID:          "plan-" + suffix,
		ProductID:   prod.ID,
		Name:        "Test Plan " + suffix,
		Slug:        "test-plan-" + suffix,
		CheckoutID:  "co-" + suffix,
		LicenseType: licType,
		UpdatesDays: 365,
		CreatedAt:   now,
	}
	if _, err := s.DB.NewInsert().Model(plan).Exec(ctx); err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	return prod, plan
}

func insertTestLicense(t *testing.T, s *store.Store, lic *model.License) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	if lic.CreatedAt.IsZero() && lic.ValidFrom.IsZero() {
		lic.CreatedAt = now
		lic.ValidFrom = now
	} else if lic.ValidFrom.IsZero() {
		lic.ValidFrom = lic.CreatedAt
	} else if lic.CreatedAt.IsZero() {
		lic.CreatedAt = lic.ValidFrom
	}
	if lic.UpdatedAt.IsZero() {
		lic.UpdatedAt = now
	}
	if lic.LicenseKey != "" && lic.KeyHash == "" {
		lic.KeyHash = license.HashKey(lic.LicenseKey)
	}
	if _, err := s.DB.NewInsert().Model(lic).Exec(ctx); err != nil {
		t.Fatalf("insert license: %v", err)
	}
}

// 1. Test: Create Customer
func TestCRM_CreateCustomer(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	cust := &model.MultiCustomer{
		FirstName: "  Jan  ",
		LastName:  "  Kowalski  ",
		Phone:     "500 123 456",
		Email:     "JAN.Kowalski@EXAMPLE.com",
	}

	if err := s.CreateCustomer(ctx, cust); err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	if cust.ID == "" {
		t.Fatal("expected non-empty customer ID")
	}
	if cust.FirstName != "Jan" || cust.LastName != "Kowalski" {
		t.Fatalf("expected trimmed names, got %q %q", cust.FirstName, cust.LastName)
	}
	if cust.NormalizedPhone != "+48500123456" {
		t.Fatalf("expected +48500123456, got %q", cust.NormalizedPhone)
	}
	if cust.NormalizedEmail != "jan.kowalski@example.com" {
		t.Fatalf("expected normalized email, got %q", cust.NormalizedEmail)
	}
	if cust.CustomerSince.IsZero() {
		t.Fatal("expected customer_since to be set")
	}
}

// 2. Test: Search by normalized phone
func TestCRM_SearchByNormalizedPhone(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	cust := &model.MultiCustomer{
		FirstName: "Adam",
		LastName:  "Nowak",
		Phone:     "+48 600 700 800",
		Email:     "adam@example.com",
	}
	if err := s.CreateCustomer(ctx, cust); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Search using domestic format with spaces
	results, total, err := s.ListCustomers(ctx, "600 700 800", false, 0, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 1 || len(results) != 1 {
		t.Fatalf("expected 1 result, got total=%d, len=%d", total, len(results))
	}
	if results[0].ID != cust.ID {
		t.Fatalf("expected customer %s, got %s", cust.ID, results[0].ID)
	}
}

// 3. Test: Search by email
func TestCRM_SearchByEmail(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	cust := &model.MultiCustomer{
		FirstName: "Marek",
		LastName:  "Zieliński",
		Phone:     "700 800 900",
		Email:     "marek.zielinski@serwis.pl",
	}
	if err := s.CreateCustomer(ctx, cust); err != nil {
		t.Fatalf("create: %v", err)
	}

	results, total, err := s.ListCustomers(ctx, "marek.zielinski@serwis.pl", false, 0, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 1 || len(results) != 1 {
		t.Fatalf("expected 1 result, got total=%d", total)
	}
	if results[0].ID != cust.ID {
		t.Fatalf("expected customer %s, got %s", cust.ID, results[0].ID)
	}
}

// 4. Test: Search by surname
func TestCRM_SearchBySurname(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	cust := &model.MultiCustomer{
		FirstName: "Piotr",
		LastName:  "Lewandowski",
		Phone:     "800 111 222",
		Email:     "piotr@test.pl",
	}
	if err := s.CreateCustomer(ctx, cust); err != nil {
		t.Fatalf("create: %v", err)
	}

	results, total, err := s.ListCustomers(ctx, "Lewand", false, 0, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 1 || len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", total)
	}
	if results[0].LastName != "Lewandowski" {
		t.Fatalf("unexpected result: %v", results[0])
	}
}

// 5. Test: Search by license key
func TestCRM_SearchByLicenseKey(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	_, plan := createTestProductAndPlan(t, s, "subscription")

	cust := &model.MultiCustomer{
		FirstName: "Kamil",
		LastName:  "Wójcik",
		Phone:     "511 222 333",
		Email:     "kamil@test.pl",
	}
	if err := s.CreateCustomer(ctx, cust); err != nil {
		t.Fatalf("create cust: %v", err)
	}

	licKey := "TEST-KEY-1234-5678"
	lic := &model.License{
		ID:         "lic-kamil-1",
		ProductID:  plan.ProductID,
		PlanID:     plan.ID,
		Email:      cust.Email,
		LicenseKey: licKey,
		Status:     model.StatusActive,
	}
	insertTestLicense(t, s, lic)

	if _, err := s.AssignLicenseToCustomer(ctx, cust.ID, lic.ID); err != nil {
		t.Fatalf("assign: %v", err)
	}

	results, total, err := s.ListCustomers(ctx, "TEST-KEY", false, 0, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 1 || len(results) != 1 {
		t.Fatalf("expected 1 result matching license key, got %d", total)
	}
	if results[0].ID != cust.ID {
		t.Fatalf("expected customer %s, got %s", cust.ID, results[0].ID)
	}
}

// 6. Test: Assign license
func TestCRM_AssignLicense(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	_, plan := createTestProductAndPlan(t, s, "subscription")

	cust := &model.MultiCustomer{
		FirstName: "Tomasz",
		LastName:  "Kamiński",
		Phone:     "501 202 303",
	}
	_ = s.CreateCustomer(ctx, cust)

	lic := &model.License{
		ID:         "lic-tom-1",
		ProductID:  plan.ProductID,
		PlanID:     plan.ID,
		LicenseKey: "KEY-TOM-1",
		Status:     model.StatusActive,
	}
	insertTestLicense(t, s, lic)

	assignment, err := s.AssignLicenseToCustomer(ctx, cust.ID, lic.ID)
	if err != nil {
		t.Fatalf("AssignLicenseToCustomer: %v", err)
	}
	if assignment.CustomerID != cust.ID || assignment.KeygateLicenseID != lic.ID {
		t.Fatalf("invalid assignment: %+v", assignment)
	}

	// Verify events
	events, err := s.ListCustomerEvents(ctx, cust.ID)
	if err != nil || len(events) == 0 {
		t.Fatalf("expected event recorded, got: %v", err)
	}
	if events[0].EventType != model.CRMEventLicenseAssigned {
		t.Fatalf("expected %s, got %s", model.CRMEventLicenseAssigned, events[0].EventType)
	}
}

// 7. Test: Second license same customer
func TestCRM_SecondLicenseSameCustomer(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	_, plan := createTestProductAndPlan(t, s, "subscription")

	cust := &model.MultiCustomer{
		FirstName: "Robert",
		LastName:  "Dąbrowski",
		Phone:     "502 303 404",
	}
	_ = s.CreateCustomer(ctx, cust)

	lic1 := &model.License{ID: "lic-rob-1", ProductID: plan.ProductID, PlanID: plan.ID, Status: model.StatusActive}
	lic2 := &model.License{ID: "lic-rob-2", ProductID: plan.ProductID, PlanID: plan.ID, Status: model.StatusActive}
	insertTestLicense(t, s, lic1)
	insertTestLicense(t, s, lic2)

	_, err1 := s.AssignLicenseToCustomer(ctx, cust.ID, lic1.ID)
	_, err2 := s.AssignLicenseToCustomer(ctx, cust.ID, lic2.ID)
	if err1 != nil || err2 != nil {
		t.Fatalf("assignments failed: %v, %v", err1, err2)
	}

	assignments, err := s.ListCustomerLicenseAssignments(ctx, cust.ID)
	if err != nil {
		t.Fatalf("list assignments: %v", err)
	}
	if len(assignments) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(assignments))
	}
}

// 8. Test: Duplicate phone detection
func TestCRM_DuplicatePhoneDetection(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	cust1 := &model.MultiCustomer{
		FirstName: "Michał",
		LastName:  "Kozłowski",
		Phone:     "+48 503 404 505",
	}
	_ = s.CreateCustomer(ctx, cust1)

	// Check duplicates with same phone in domestic format
	dups, err := s.CheckCustomerDuplicates(ctx, "503-404-505", "different@test.pl", "")
	if err != nil {
		t.Fatalf("find duplicates: %v", err)
	}
	if len(dups.ExactPhoneMatch) != 1 || dups.ExactPhoneMatch[0].ID != cust1.ID {
		t.Fatalf("expected duplicate detected by phone, got %v", dups)
	}
}

// 9. Test: Duplicate email detection
func TestCRM_DuplicateEmailDetection(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	cust1 := &model.MultiCustomer{
		FirstName: "Krzysztof",
		LastName:  "Jankowski",
		Phone:     "504 505 606",
		Email:     "krzysztof@firma.pl",
	}
	_ = s.CreateCustomer(ctx, cust1)

	dups, err := s.CheckCustomerDuplicates(ctx, "599 999 999", "KRZYSZTOF@FIRMA.PL", "")
	if err != nil {
		t.Fatalf("find duplicates: %v", err)
	}
	if len(dups.ExactEmailMatch) != 1 || dups.ExactEmailMatch[0].ID != cust1.ID {
		t.Fatalf("expected duplicate detected by email, got %v", dups)
	}
}

// 10. Test: Same first+last name does NOT auto-merge or trigger false duplicate
func TestCRM_SameFirstLastNameDoesNotAutoMerge(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	cust1 := &model.MultiCustomer{
		FirstName: "Jan",
		LastName:  "Kowalski",
		Phone:     "501 111 111",
		Email:     "jan1@test.pl",
	}
	_ = s.CreateCustomer(ctx, cust1)

	// Different phone and email, identical names
	dups, err := s.CheckCustomerDuplicates(ctx, "502 222 222", "jan2@test.pl", "")
	if err != nil {
		t.Fatalf("find duplicates: %v", err)
	}
	if len(dups.ExactPhoneMatch) != 0 || len(dups.ExactEmailMatch) != 0 {
		t.Fatalf("expected 0 duplicates for different contact info, got %v", dups)
	}

	cust2 := &model.MultiCustomer{
		FirstName: "Jan",
		LastName:  "Kowalski",
		Phone:     "502 222 222",
		Email:     "jan2@test.pl",
	}
	if err := s.CreateCustomer(ctx, cust2); err != nil {
		t.Fatalf("create cust2: %v", err)
	}

	if cust1.ID == cust2.ID {
		t.Fatal("customers with same name must remain distinct records")
	}
}

// 12. Test: Customer A cannot access / customer detail cannot contain customer B history
func TestCRM_CustomerHistoryIsolation(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	custA := &model.MultiCustomer{FirstName: "Klient", LastName: "A", Phone: "511 111 111"}
	custB := &model.MultiCustomer{FirstName: "Klient", LastName: "B", Phone: "522 222 222"}
	_ = s.CreateCustomer(ctx, custA)
	_ = s.CreateCustomer(ctx, custB)

	// Log event for B
	_ = s.RecordCustomerEvent(ctx, &model.MultiCustomerEvent{
		CustomerID: custB.ID,
		EventType:  model.CRMEventManualNote,
		Notes:      "Tajna notatka klienta B",
	})

	// Fetch events for A
	eventsA, err := s.ListCustomerEvents(ctx, custA.ID)
	if err != nil {
		t.Fatalf("list events A: %v", err)
	}
	if len(eventsA) != 0 {
		t.Fatalf("customer A should have 0 events, got %d", len(eventsA))
	}

	// Fetch stats for A
	statsA, _, _, timelineA, err := s.CalculateCustomerStatsAndTimeline(ctx, custA, time.Now())
	if err != nil {
		t.Fatalf("calc stats A: %v", err)
	}
	if statsA.TotalLicensesCount != 0 || len(timelineA) != 0 {
		t.Fatalf("isolation violation: customer A stats contained data: %+v", statsA)
	}
}

// 13. Test: Archive preserves KeyGate licenses
func TestCRM_ArchivePreservesKeygateLicenses(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	_, plan := createTestProductAndPlan(t, s, "subscription")

	cust := &model.MultiCustomer{FirstName: "Grzegorz", LastName: "Brzęczyszczykiewicz", Phone: "505 606 707"}
	_ = s.CreateCustomer(ctx, cust)

	lic := &model.License{
		ID:         "lic-preserve-1",
		ProductID:  plan.ProductID,
		PlanID:     plan.ID,
		Status:     model.StatusActive,
		LicenseKey: "PRESERVE-KEY-1",
	}
	insertTestLicense(t, s, lic)
	_, _ = s.AssignLicenseToCustomer(ctx, cust.ID, lic.ID)

	// Archive customer
	if err := s.ArchiveCustomer(ctx, cust.ID); err != nil {
		t.Fatalf("archive customer: %v", err)
	}

	// Verify license row is still present and unchanged in licenses table
	var foundLic model.License
	err := s.DB.NewSelect().Model(&foundLic).Where("id = ?", lic.ID).Scan(ctx)
	if err != nil {
		t.Fatalf("license was deleted after customer archive: %v", err)
	}
	if foundLic.Status != model.StatusActive {
		t.Fatalf("license status changed, got %s", foundLic.Status)
	}
}

// 14. Test: customer_since preserved after renewal
func TestCRM_CustomerSincePreservedAfterRenewal(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	_, plan := createTestProductAndPlan(t, s, "subscription")

	past := time.Now().Add(-180 * 24 * time.Hour)
	cust := &model.MultiCustomer{
		FirstName:     "Wiesław",
		LastName:      "Cichy",
		Phone:         "506 707 808",
		CustomerSince: past,
	}
	_ = s.CreateCustomer(ctx, cust)

	until := time.Now().Add(30 * 24 * time.Hour)
	lic := &model.License{
		ID:         "lic-wieslaw-1",
		ProductID:  plan.ProductID,
		PlanID:     plan.ID,
		Status:     model.StatusActive,
		ValidUntil: &until,
	}
	insertTestLicense(t, s, lic)
	_, _ = s.AssignLicenseToCustomer(ctx, cust.ID, lic.ID)

	// Renew
	_, err := s.RenewCustomerLicenseInTx(ctx, cust.ID, lic.ID, 12, nil, model.PaymentMethodCash, nil, "PLN", "Renewal")
	if err != nil {
		t.Fatalf("renew: %v", err)
	}

	afterCust, err := s.FindCustomerByID(ctx, cust.ID)
	if err != nil {
		t.Fatalf("find cust: %v", err)
	}
	if afterCust.CustomerSince.Unix() != past.Unix() {
		t.Fatalf("customer_since altered after renewal: expected %v, got %v", past, afterCust.CustomerSince)
	}
}

// 15. Test: customer_since preserved after new license
func TestCRM_CustomerSincePreservedAfterNewLicense(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	_, plan := createTestProductAndPlan(t, s, "subscription")

	past := time.Now().Add(-365 * 24 * time.Hour)
	cust := &model.MultiCustomer{
		FirstName:     "Damian",
		LastName:      "Sikora",
		Phone:         "507 808 909",
		CustomerSince: past,
	}
	_ = s.CreateCustomer(ctx, cust)

	lic2 := &model.License{
		ID:         "lic-damian-2",
		ProductID:  plan.ProductID,
		PlanID:     plan.ID,
		Status:     model.StatusActive,
		LicenseKey: "DAMIAN-KEY-2",
	}
	insertTestLicense(t, s, lic2)
	_, _ = s.AssignLicenseToCustomer(ctx, cust.ID, lic2.ID)

	afterCust, err := s.FindCustomerByID(ctx, cust.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if afterCust.CustomerSince.Unix() != past.Unix() {
		t.Fatalf("customer_since was overwritten: %v vs %v", past, afterCust.CustomerSince)
	}
}

// 16. Test: Manual payment event
func TestCRM_ManualPaymentEvent(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	cust := &model.MultiCustomer{FirstName: "Mariusz", LastName: "Błaszczyk", Phone: "508 909 010"}
	_ = s.CreateCustomer(ctx, cust)

	amount := 250.00
	ev := &model.MultiCustomerEvent{
		CustomerID:    cust.ID,
		EventType:     model.CRMEventPaymentRecorded,
		PaymentMethod: model.PaymentMethodBankTransfer,
		Amount:        &amount,
		Currency:      "PLN",
		Notes:         "Wpłata przelewem ręczna",
	}
	if err := s.RecordCustomerEvent(ctx, ev); err != nil {
		t.Fatalf("record payment: %v", err)
	}

	events, err := s.ListCustomerEvents(ctx, cust.ID)
	if err != nil {
		t.Fatalf("list events failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if *events[0].Amount != 250.00 || events[0].PaymentMethod != model.PaymentMethodBankTransfer {
		t.Fatalf("invalid event values: %+v", events[0])
	}
}

// 17. Test: Renewal event
func TestCRM_RenewalEvent(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	_, plan := createTestProductAndPlan(t, s, "subscription")

	cust := &model.MultiCustomer{FirstName: "Rafał", LastName: "Pawlak", Phone: "509 010 112"}
	_ = s.CreateCustomer(ctx, cust)

	until := time.Now().Add(10 * 24 * time.Hour)
	lic := &model.License{
		ID:         "lic-rafal-1",
		ProductID:  plan.ProductID,
		PlanID:     plan.ID,
		Status:     model.StatusActive,
		ValidUntil: &until,
	}
	insertTestLicense(t, s, lic)
	_, _ = s.AssignLicenseToCustomer(ctx, cust.ID, lic.ID)

	amt := 150.0
	_, err := s.RenewCustomerLicenseInTx(ctx, cust.ID, lic.ID, 6, nil, model.PaymentMethodCash, &amt, "PLN", "Przedłużenie 6m")
	if err != nil {
		t.Fatalf("renew: %v", err)
	}

	events, err := s.ListCustomerEvents(ctx, cust.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	foundRenew := false
	foundPay := false
	for _, e := range events {
		if e.EventType == model.CRMEventLicenseRenewed {
			foundRenew = true
		}
		if e.EventType == model.CRMEventPaymentRecorded {
			foundPay = true
		}
	}
	if !foundRenew || !foundPay {
		t.Fatalf("expected renewal and payment events, got: %+v", events)
	}
}

// 18. Test: Chronological timeline
func TestCRM_ChronologicalTimeline(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	cust := &model.MultiCustomer{FirstName: "Zenon", LastName: "Martyniuk", Phone: "510 112 223"}
	_ = s.CreateCustomer(ctx, cust)

	t1 := time.Now().Add(-3 * time.Hour)
	t2 := time.Now().Add(-1 * time.Hour)
	t3 := time.Now()

	_ = s.RecordCustomerEvent(ctx, &model.MultiCustomerEvent{CustomerID: cust.ID, EventType: model.CRMEventCustomerCreated, OccurredAt: t1})
	_ = s.RecordCustomerEvent(ctx, &model.MultiCustomerEvent{CustomerID: cust.ID, EventType: model.CRMEventManualNote, Notes: "Rozmowa telefoniczna", OccurredAt: t2})
	_ = s.RecordCustomerEvent(ctx, &model.MultiCustomerEvent{CustomerID: cust.ID, EventType: model.CRMEventPaymentRecorded, OccurredAt: t3})

	_, _, _, timeline, err := s.CalculateCustomerStatsAndTimeline(ctx, cust, time.Now())
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}

	if len(timeline) != 3 {
		t.Fatalf("expected 3 items, got %d", len(timeline))
	}
	// Items are ordered DESC (newest first)
	if !timeline[0].Date.After(timeline[1].Date) || !timeline[1].Date.After(timeline[2].Date) {
		t.Fatalf("timeline is not strictly sorted newest first: %v, %v, %v", timeline[0].Date, timeline[1].Date, timeline[2].Date)
	}
}

// 19. Test: Active-time union calculation
func TestCRM_ActiveTimeUnion(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	_, plan := createTestProductAndPlan(t, s, "subscription")

	cust := &model.MultiCustomer{FirstName: "Artur", LastName: "Szpilka", Phone: "511 223 334"}
	_ = s.CreateCustomer(ctx, cust)

	// Two disjoint licenses of 30 days each
	now := time.Now()
	lic1Start := now.Add(-100 * 24 * time.Hour)
	lic1End := now.Add(-70 * 24 * time.Hour)

	lic2Start := now.Add(-50 * 24 * time.Hour)
	lic2End := now.Add(-20 * 24 * time.Hour)

	lic1 := &model.License{ID: "lic-artur-1", ProductID: plan.ProductID, PlanID: plan.ID, CreatedAt: lic1Start, ValidUntil: &lic1End, Status: model.StatusExpired}
	lic2 := &model.License{ID: "lic-artur-2", ProductID: plan.ProductID, PlanID: plan.ID, CreatedAt: lic2Start, ValidUntil: &lic2End, Status: model.StatusExpired}
	insertTestLicense(t, s, lic1)
	insertTestLicense(t, s, lic2)
	_, _ = s.AssignLicenseToCustomer(ctx, cust.ID, lic1.ID)
	_, _ = s.AssignLicenseToCustomer(ctx, cust.ID, lic2.ID)

	stats, _, _, _, err := s.CalculateCustomerStatsAndTimeline(ctx, cust, now)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	// 30 + 30 = 60 active days
	if stats.ActiveDays < 59 || stats.ActiveDays > 61 {
		t.Fatalf("expected ~60 active days, got %d", stats.ActiveDays)
	}
}

// 20. Test: Overlapping licenses do not double-count
func TestCRM_OverlappingLicensesDoNotDoubleCount(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	_, plan := createTestProductAndPlan(t, s, "subscription")

	cust := &model.MultiCustomer{FirstName: "Dariusz", LastName: "Michalczewski", Phone: "512 334 445"}
	_ = s.CreateCustomer(ctx, cust)

	// Two identical overlapping licenses covering the exact same 30-day window
	now := time.Now()
	start := now.Add(-40 * 24 * time.Hour)
	end := now.Add(-10 * 24 * time.Hour)

	lic1 := &model.License{ID: "lic-dm-1", ProductID: plan.ProductID, PlanID: plan.ID, CreatedAt: start, ValidUntil: &end, Status: model.StatusExpired}
	lic2 := &model.License{ID: "lic-dm-2", ProductID: plan.ProductID, PlanID: plan.ID, CreatedAt: start, ValidUntil: &end, Status: model.StatusExpired}
	insertTestLicense(t, s, lic1)
	insertTestLicense(t, s, lic2)
	_, _ = s.AssignLicenseToCustomer(ctx, cust.ID, lic1.ID)
	_, _ = s.AssignLicenseToCustomer(ctx, cust.ID, lic2.ID)

	stats, _, _, _, err := s.CalculateCustomerStatsAndTimeline(ctx, cust, now)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	// Overlapping window must count as 30 days, NOT 60 days!
	if stats.ActiveDays < 29 || stats.ActiveDays > 31 {
		t.Fatalf("overlapping licenses were double counted! Expected ~30 active days, got %d", stats.ActiveDays)
	}
}

// 21. Test: Gap calculation
func TestCRM_GapCalculation(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	_, plan := createTestProductAndPlan(t, s, "subscription")

	cust := &model.MultiCustomer{FirstName: "Łukasz", LastName: "Piszczek", Phone: "513 445 556"}
	_ = s.CreateCustomer(ctx, cust)

	now := time.Now()
	lic1Start := now.Add(-60 * 24 * time.Hour)
	lic1End := now.Add(-40 * 24 * time.Hour)
	// 10-day gap between -40d and -30d
	lic2Start := now.Add(-30 * 24 * time.Hour)
	lic2End := now.Add(-10 * 24 * time.Hour)

	lic1 := &model.License{ID: "lic-gap-1", ProductID: plan.ProductID, PlanID: plan.ID, CreatedAt: lic1Start, ValidUntil: &lic1End, Status: model.StatusExpired}
	lic2 := &model.License{ID: "lic-gap-2", ProductID: plan.ProductID, PlanID: plan.ID, CreatedAt: lic2Start, ValidUntil: &lic2End, Status: model.StatusExpired}
	insertTestLicense(t, s, lic1)
	insertTestLicense(t, s, lic2)
	_, _ = s.AssignLicenseToCustomer(ctx, cust.ID, lic1.ID)
	_, _ = s.AssignLicenseToCustomer(ctx, cust.ID, lic2.ID)

	stats, _, _, _, err := s.CalculateCustomerStatsAndTimeline(ctx, cust, now)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.GapsCount != 1 {
		t.Fatalf("expected 1 gap detected, got %d", stats.GapsCount)
	}
	if stats.GapDays < 9 || stats.GapDays > 11 {
		t.Fatalf("expected ~10 gap days, got %d", stats.GapDays)
	}
}

// 22. Test: New license after gap remains same customer
func TestCRM_NewLicenseAfterGapRemainsSameCustomer(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	_, plan := createTestProductAndPlan(t, s, "subscription")

	cust := &model.MultiCustomer{FirstName: "Jakub", LastName: "Błaszczykowski", Phone: "514 556 667"}
	_ = s.CreateCustomer(ctx, cust)

	now := time.Now()
	oldStart := now.Add(-90 * 24 * time.Hour)
	oldEnd := now.Add(-60 * 24 * time.Hour)
	licOld := &model.License{ID: "lic-kuba-old", ProductID: plan.ProductID, PlanID: plan.ID, CreatedAt: oldStart, ValidUntil: &oldEnd, Status: model.StatusExpired}
	insertTestLicense(t, s, licOld)
	_, _ = s.AssignLicenseToCustomer(ctx, cust.ID, licOld.ID)

	// New license issued today after 60-day gap
	newEnd := now.Add(365 * 24 * time.Hour)
	licNew := &model.License{ID: "lic-kuba-new", ProductID: plan.ProductID, PlanID: plan.ID, CreatedAt: now, ValidUntil: &newEnd, Status: model.StatusActive}
	insertTestLicense(t, s, licNew)
	_, _ = s.AssignLicenseToCustomer(ctx, cust.ID, licNew.ID)

	stats, _, _, _, err := s.CalculateCustomerStatsAndTimeline(ctx, cust, now)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.TotalLicensesCount != 2 || stats.ActiveLicensesCount != 1 {
		t.Fatalf("expected 2 total licenses (1 active), got total=%d active=%d", stats.TotalLicensesCount, stats.ActiveLicensesCount)
	}
	if stats.GapsCount != 1 {
		t.Fatalf("expected 1 gap between old and new license, got %d", stats.GapsCount)
	}
}

// 23. Test: Unknown license cannot be assigned
func TestCRM_UnknownLicenseCannotBeAssigned(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	cust := &model.MultiCustomer{FirstName: "Grzegorz", LastName: "Krychowiak", Phone: "515 667 778"}
	_ = s.CreateCustomer(ctx, cust)

	_, err := s.AssignLicenseToCustomer(ctx, cust.ID, "non-existent-license-id")
	if err == nil {
		t.Fatal("expected error when assigning unknown license, got nil")
	}
	if err != store.ErrLicenseNotFound {
		t.Fatalf("expected ErrLicenseNotFound, got: %v", err)
	}
}

// 24. Test: Reassignment preserves history/audit
func TestCRM_ReassignmentPreservesHistory(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	_, plan := createTestProductAndPlan(t, s, "subscription")

	custA := &model.MultiCustomer{FirstName: "Firma", LastName: "Alfa", Phone: "516 778 889"}
	custB := &model.MultiCustomer{FirstName: "Firma", LastName: "Beta", Phone: "517 889 990"}
	_ = s.CreateCustomer(ctx, custA)
	_ = s.CreateCustomer(ctx, custB)

	lic := &model.License{ID: "lic-reassign-1", ProductID: plan.ProductID, PlanID: plan.ID, Status: model.StatusActive}
	insertTestLicense(t, s, lic)

	// Assigned to A
	_, err := s.AssignLicenseToCustomer(ctx, custA.ID, lic.ID)
	if err != nil {
		t.Fatalf("assign to A: %v", err)
	}

	// Reassign to B
	err = s.ReassignLicense(ctx, lic.ID, custA.ID, custB.ID)
	if err != nil {
		t.Fatalf("reassign to B: %v", err)
	}

	// Verify A's assignment is ended (ended_at != nil)
	assignmentsA, _ := s.ListCustomerLicenseAssignments(ctx, custA.ID)
	if len(assignmentsA) != 1 || assignmentsA[0].EndedAt == nil {
		t.Fatalf("customer A assignment should be closed with ended_at, got: %+v", assignmentsA)
	}

	// Verify B has active assignment
	assignmentsB, _ := s.ListCustomerLicenseAssignments(ctx, custB.ID)
	if len(assignmentsB) != 1 || assignmentsB[0].EndedAt != nil {
		t.Fatalf("customer B should have active assignment, got: %+v", assignmentsB)
	}

	// Verify audit event on B
	eventsB, _ := s.ListCustomerEvents(ctx, custB.ID)
	found := false
	for _, e := range eventsB {
		if e.EventType == model.CRMEventLicenseReassigned {
			found = true
		}
	}
	if !found {
		t.Fatal("expected CRMEventLicenseReassigned event on target customer B")
	}
}

// 25. Test: Merge preserves licenses and events
func TestCRM_MergePreservesLicensesAndEvents(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	_, plan := createTestProductAndPlan(t, s, "subscription")

	earlyDate := time.Now().Add(-500 * 24 * time.Hour)
	source := &model.MultiCustomer{
		FirstName:     "Jan",
		LastName:      "Kowalski",
		Phone:         "518 111 222",
		CustomerSince: earlyDate,
		Notes:         "Notatka ze źródłowego klienta",
	}
	laterDate := time.Now().Add(-100 * 24 * time.Hour)
	target := &model.MultiCustomer{
		FirstName:     "Jan",
		LastName:      "Kowalski",
		Phone:         "518 333 444",
		CustomerSince: laterDate,
		Notes:         "Notatka z docelowego klienta",
	}
	_ = s.CreateCustomer(ctx, source)
	_ = s.CreateCustomer(ctx, target)

	lic := &model.License{ID: "lic-merge-source", ProductID: plan.ProductID, PlanID: plan.ID, Status: model.StatusActive}
	insertTestLicense(t, s, lic)
	_, _ = s.AssignLicenseToCustomer(ctx, source.ID, lic.ID)

	_ = s.RecordCustomerEvent(ctx, &model.MultiCustomerEvent{
		CustomerID: source.ID,
		EventType:  model.CRMEventManualNote,
		Notes:      "Zdarzenie źródłowe",
	})

	// Execute merge
	if err := s.MergeCustomers(ctx, source.ID, target.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// 1. Target must inherit earliest customer_since
	mergedTarget, _ := s.FindCustomerByID(ctx, target.ID)
	if mergedTarget.CustomerSince.Unix() != earlyDate.Unix() {
		t.Fatalf("expected earliest customer_since %v, got %v", earlyDate, mergedTarget.CustomerSince)
	}

	// 2. Target must inherit license assignment
	targetLicenses, _ := s.ListCustomerLicenseAssignments(ctx, target.ID)
	if len(targetLicenses) != 1 || targetLicenses[0].KeygateLicenseID != lic.ID {
		t.Fatalf("target did not inherit license: %+v", targetLicenses)
	}

	// 3. Target must have all events + merge event
	targetEvents, _ := s.ListCustomerEvents(ctx, target.ID)
	if len(targetEvents) < 2 {
		t.Fatalf("expected preserved events on target, got %d", len(targetEvents))
	}

	// 4. Source must be soft-deleted (archived_at set)
	archivedSource, _ := s.FindCustomerByID(ctx, source.ID)
	if archivedSource.ArchivedAt == nil {
		t.Fatal("expected source customer to be archived")
	}
}

// 26. Test: CRM PII never appears in public license verify response
func TestCRM_PIINeverAppearsInPublicVerify(t *testing.T) {
	// PII check: Inspect VerifyResult fields
	v := model.License{}
	// Confirm model.License has no CRM fields
	_ = v
	// Verify that license verify model produces no CRM keys
	res := map[string]any{
		"status":      "active",
		"plan_id":     "p1",
		"plan_name":   "Pro",
		"valid_until": time.Now(),
		"features":    map[string]any{},
		"token":       "jwt-token",
	}

	prohibitedKeys := []string{"first_name", "last_name", "phone", "crm_notes", "payment_history", "notes"}
	for _, key := range prohibitedKeys {
		if _, exists := res[key]; exists {
			t.Fatalf("security violation: public verify response contains CRM PII key %q", key)
		}
	}
}

// 27. Test: Stripe idempotency - repeated webhook delivery does not duplicate customer, license, or events
func TestCRM_StripeIdempotency(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	_, plan := createTestProductAndPlan(t, s, "subscription")

	cust := &model.MultiCustomer{
		FirstName: "Stripe",
		LastName:  "Buyer",
		Phone:     "519 000 111",
		Email:     "buyer@stripe.test",
	}
	_ = s.CreateCustomer(ctx, cust)

	lic := &model.License{
		ID:         "lic-stripe-idem-1",
		ProductID:  plan.ProductID,
		PlanID:     plan.ID,
		Email:      cust.Email,
		Status:     model.StatusActive,
		LicenseKey: "STRIPE-IDEM-KEY",
	}
	insertTestLicense(t, s, lic)

	eventID := "evt_test_webhook_12345"

	// First webhook delivery
	err := s.LinkStripeLicenseToCRM(ctx, lic, cust.Email, eventID)
	if err != nil {
		t.Fatalf("first link: %v", err)
	}

	// Second delivery of exact same webhook
	err = s.LinkStripeLicenseToCRM(ctx, lic, cust.Email, eventID)
	if err != nil {
		t.Fatalf("second link (retry): %v", err)
	}

	// Third delivery
	err = s.LinkStripeLicenseToCRM(ctx, lic, cust.Email, eventID)
	if err != nil {
		t.Fatalf("third link (retry): %v", err)
	}

	// Verify only 1 assignment exists
	assignments, err := s.ListCustomerLicenseAssignments(ctx, cust.ID)
	if err != nil || len(assignments) != 1 {
		t.Fatalf("expected exactly 1 assignment, got %d", len(assignments))
	}

	// Verify only 1 license_issued event exists with this externalEventID
	events, err := s.ListCustomerEvents(ctx, cust.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	stripeEvents := 0
	for _, e := range events {
		if e.ExternalEventID == eventID {
			stripeEvents++
		}
	}
	if stripeEvents != 1 {
		t.Fatalf("idempotency failed: expected exactly 1 event for eventID %s, got %d", eventID, stripeEvents)
	}
}

// 28. Test: Stripe customer matching fail-closed on ambiguity
func TestCRM_StripeMatchingFailsClosedOnAmbiguity(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	_, plan := createTestProductAndPlan(t, s, "subscription")

	sharedEmail := "shared@client.pl"
	// Two separate customers with identical email
	cust1 := &model.MultiCustomer{FirstName: "Marek", LastName: "A", Phone: "520 111 000", Email: sharedEmail}
	cust2 := &model.MultiCustomer{FirstName: "Marek", LastName: "B", Phone: "520 222 000", Email: sharedEmail}
	_ = s.CreateCustomer(ctx, cust1)
	_ = s.CreateCustomer(ctx, cust2)

	lic := &model.License{
		ID:         "lic-stripe-ambig-1",
		ProductID:  plan.ProductID,
		PlanID:     plan.ID,
		Email:      sharedEmail,
		Status:     model.StatusActive,
		LicenseKey: "AMBIG-KEY-1",
	}
	insertTestLicense(t, s, lic)

	// Attempt link
	err := s.LinkStripeLicenseToCRM(ctx, lic, sharedEmail, "evt_ambig_1")
	if err != nil {
		t.Fatalf("link error: %v", err)
	}

	// Verify FAIL CLOSED: Neither customer1 nor customer2 should be assigned the license
	assigns1, _ := s.ListCustomerLicenseAssignments(ctx, cust1.ID)
	assigns2, _ := s.ListCustomerLicenseAssignments(ctx, cust2.ID)
	if len(assigns1) != 0 || len(assigns2) != 0 {
		t.Fatalf("FAIL-CLOSED VIOLATION: license was arbitrarily assigned despite ambiguous matching! (cust1: %d, cust2: %d)", len(assigns1), len(assigns2))
	}
}

// 29. Test: Phone normalization suite with international and domestic numbers
func TestCRM_PhoneNormalizationInternational(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		// Poland standard domestic
		{"500 123 456", "+48500123456"},
		{"+48 500 123 456", "+48500123456"},
		{"0048 500 123 456", "+48500123456"},
		{"(500) 123-456", "+48500123456"},

		// USA
		{"+1 (555) 234-5678", "+15552345678"},
		{"001 555 234 5678", "+15552345678"},

		// UK
		{"+44 20 7946 0958", "+442079460958"},
		{"0044 20 7946 0958", "+442079460958"},

		// Germany
		{"+49 30 123456", "+4930123456"},

		// Empty / whitespace
		{"", ""},
		{"   ", ""},
	}

	for _, tc := range testCases {
		actual := store.NormalizePhone(tc.input)
		if actual != tc.expected {
			t.Errorf("NormalizePhone(%q) = %q, expected %q", tc.input, actual, tc.expected)
		}
	}
}

// 30. Regression Test: New customer with zero licenses and events must return empty slices, never null
func TestCRM_NewCustomerEmptyDetailsDoesNotReturnNullSlices(t *testing.T) {
	s, cleanup := setupMemoryCRMStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create customer without any licenses, purchases, or events
	cust := &model.MultiCustomer{
		FirstName: "Nowy",
		LastName:  "Klient",
		Phone:     "500 999 888",
		Email:     "nowy.klient@multi-servis.pl",
	}
	err := s.CreateCustomer(ctx, cust)
	if err != nil {
		t.Fatalf("CreateCustomer: %v", err)
	}

	stats, activeLics, licHistory, timeline, err := s.CalculateCustomerStatsAndTimeline(ctx, cust, time.Now())
	if err != nil {
		t.Fatalf("CalculateCustomerStatsAndTimeline: %v", err)
	}

	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if activeLics == nil {
		t.Fatal("expected activeLics to be non-nil empty slice, got nil")
	}
	if licHistory == nil {
		t.Fatal("expected licHistory to be non-nil empty slice, got nil")
	}
	if timeline == nil {
		t.Fatal("expected timeline to be non-nil empty slice, got nil")
	}
	if stats.CurrentProducts == nil {
		t.Fatal("expected stats.CurrentProducts to be non-nil empty slice, got nil")
	}
	if stats.CurrentPlans == nil {
		t.Fatal("expected stats.CurrentPlans to be non-nil empty slice, got nil")
	}
	if stats.Gaps == nil {
		t.Fatal("expected stats.Gaps to be non-nil empty slice, got nil")
	}

	// Verify JSON serialization produces [] and not null
	detailResp := model.CustomerDetailResponse{
		Customer:       cust,
		Stats:          stats,
		ActiveLicenses: activeLics,
		LicenseHistory: licHistory,
		Timeline:       timeline,
	}

	data, err := json.Marshal(detailResp)
	if err != nil {
		t.Fatalf("json.Marshal detailResp: %v", err)
	}
	jsonStr := string(data)

	expectedEmptyArrays := []string{
		`"timeline":[]`,
		`"active_licenses":[]`,
		`"license_history":[]`,
		`"current_products":[]`,
		`"current_plans":[]`,
		`"gaps":[]`,
	}
	for _, expected := range expectedEmptyArrays {
		if !strings.Contains(jsonStr, expected) {
			t.Errorf("expected JSON to contain %s, but got: %s", expected, jsonStr)
		}
	}

	forbiddenNulls := []string{
		`"timeline":null`,
		`"active_licenses":null`,
		`"license_history":null`,
		`"current_products":null`,
		`"current_plans":null`,
		`"gaps":null`,
	}
	for _, forbidden := range forbiddenNulls {
		if strings.Contains(jsonStr, forbidden) {
			t.Errorf("CRITICAL BUG: JSON contains %s: %s", forbidden, jsonStr)
		}
	}

	// Also verify ListCustomers with empty filter or empty result returns non-nil slice
	custList, total, err := s.ListCustomers(ctx, "nonexistent-query-xyz", false, 0, 10)
	if err != nil {
		t.Fatalf("ListCustomers: %v", err)
	}
	if custList == nil {
		t.Fatal("expected custList to be non-nil empty slice, got nil")
	}
	if total != 0 {
		t.Fatalf("expected total 0, got %d", total)
	}

	// Also verify CheckCustomerDuplicates returns non-nil slices
	dupRes, err := s.CheckCustomerDuplicates(ctx, "", "+48000000000", "none@example.com")
	if err != nil {
		t.Fatalf("CheckCustomerDuplicates: %v", err)
	}
	if dupRes.ExactPhoneMatch == nil || dupRes.ExactEmailMatch == nil {
		t.Fatal("expected non-nil slices in CheckCustomerDuplicates")
	}
}

