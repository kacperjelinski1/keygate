package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/nyaruka/phonenumbers"
	"github.com/uptrace/bun"

	"github.com/tabloy/keygate/internal/license"
	"github.com/tabloy/keygate/internal/model"
)

var (
	ErrCustomerNotFound = errors.New("customer not found")
	ErrLicenseNotFound  = errors.New("license not found")
)

// ─── Normalization Helpers ───

// NormalizePhone canonicalizes a phone number to E.164 format.
// It uses Google libphonenumber with default region "PL".
// Domestic Polish numbers without country code receive +48.
// International numbers with country code/prefix (+ or 00) are correctly preserved.
func NormalizePhone(phone string) string {
	raw := strings.TrimSpace(phone)
	if raw == "" {
		return ""
	}

	num, err := phonenumbers.Parse(raw, "PL")
	if err == nil && num != nil {
		if phonenumbers.IsValidNumber(num) {
			return phonenumbers.Format(num, phonenumbers.E164)
		}
		formatted := phonenumbers.Format(num, phonenumbers.E164)
		if formatted != "" {
			return formatted
		}
	}

	// Fallback for non-standard or raw input
	if strings.HasPrefix(raw, "00") {
		raw = "+" + raw[2:]
	}
	var sb strings.Builder
	for _, r := range raw {
		if unicode.IsDigit(r) || (r == '+' && sb.Len() == 0) {
			sb.WriteRune(r)
		}
	}
	digits := sb.String()
	if digits == "" {
		return ""
	}
	if len(digits) == 9 && !strings.HasPrefix(digits, "+") {
		return "+48" + digits
	}
	if !strings.HasPrefix(digits, "+") && len(digits) > 9 {
		return "+" + digits
	}
	return digits
}

// NormalizeEmail canonicalizes email for comparison.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ─── Customer Operations ───

// CreateCustomer creates a new CRM customer.
func (s *Store) CreateCustomer(ctx context.Context, c *model.MultiCustomer) error {
	if c.ID == "" {
		c.ID = newID()
	}
	c.FirstName = strings.TrimSpace(c.FirstName)
	c.LastName = strings.TrimSpace(c.LastName)
	c.Phone = strings.TrimSpace(c.Phone)
	c.Email = strings.TrimSpace(c.Email)

	if c.FirstName == "" || c.LastName == "" {
		return errors.New("first_name and last_name are required")
	}
	if c.Phone == "" {
		return errors.New("phone is required")
	}

	c.NormalizedPhone = NormalizePhone(c.Phone)
	c.NormalizedEmail = NormalizeEmail(c.Email)

	now := time.Now()
	if c.CustomerSince.IsZero() {
		c.CustomerSince = now
	}
	c.CreatedAt = now
	c.UpdatedAt = now

	_, err := s.DB.NewInsert().Model(c).Exec(ctx)
	return err
}

// FindCustomerByID fetches a customer by ID.
func (s *Store) FindCustomerByID(ctx context.Context, id string) (*model.MultiCustomer, error) {
	c := new(model.MultiCustomer)
	err := s.DB.NewSelect().Model(c).Where("id = ?", id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCustomerNotFound
	}
	return c, err
}

// UpdateCustomer updates customer profile details.
func (s *Store) UpdateCustomer(ctx context.Context, c *model.MultiCustomer, cols ...string) error {
	c.FirstName = strings.TrimSpace(c.FirstName)
	c.LastName = strings.TrimSpace(c.LastName)
	c.Phone = strings.TrimSpace(c.Phone)
	c.Email = strings.TrimSpace(c.Email)
	c.NormalizedPhone = NormalizePhone(c.Phone)
	c.NormalizedEmail = NormalizeEmail(c.Email)
	c.UpdatedAt = time.Now()

	updateCols := []string{
		"first_name", "last_name", "phone", "normalized_phone",
		"email", "normalized_email", "customer_since", "notes", "updated_at",
	}
	if len(cols) > 0 {
		updateCols = append(cols, "updated_at")
	}

	res, err := s.DB.NewUpdate().Model(c).Column(updateCols...).WherePK().Exec(ctx)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrCustomerNotFound
	}
	return nil
}

// ArchiveCustomer sets archived_at timestamp on customer (soft delete).
func (s *Store) ArchiveCustomer(ctx context.Context, id string) error {
	now := time.Now()
	res, err := s.DB.NewUpdate().
		Model((*model.MultiCustomer)(nil)).
		Set("archived_at = ?", now).
		Set("updated_at = ?", now).
		Where("id = ? AND archived_at IS NULL", id).
		Exec(ctx)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrCustomerNotFound
	}
	return nil
}

// CheckCustomerDuplicates looks up existing customers matching exact normalized phone or email.
func (s *Store) CheckCustomerDuplicates(ctx context.Context, phone, email, excludeID string) (*model.CheckDuplicateResult, error) {
	normPhone := NormalizePhone(phone)
	normEmail := NormalizeEmail(email)

	res := &model.CheckDuplicateResult{
		ExactPhoneMatch: make([]*model.MultiCustomer, 0),
		ExactEmailMatch: make([]*model.MultiCustomer, 0),
	}

	if normPhone != "" {
		q := s.DB.NewSelect().Model(&res.ExactPhoneMatch).
			Where("normalized_phone = ? AND archived_at IS NULL", normPhone)
		if excludeID != "" {
			q = q.Where("id != ?", excludeID)
		}
		if err := q.Scan(ctx); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	if normEmail != "" {
		q := s.DB.NewSelect().Model(&res.ExactEmailMatch).
			Where("normalized_email = ? AND archived_at IS NULL", normEmail)
		if excludeID != "" {
			q = q.Where("id != ?", excludeID)
		}
		if err := q.Scan(ctx); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	if res.ExactPhoneMatch == nil {
		res.ExactPhoneMatch = make([]*model.MultiCustomer, 0)
	}
	if res.ExactEmailMatch == nil {
		res.ExactEmailMatch = make([]*model.MultiCustomer, 0)
	}

	res.HasDuplicates = len(res.ExactPhoneMatch) > 0 || len(res.ExactEmailMatch) > 0
	return res, nil
}

// ListCustomers queries customers with search filtering and pagination.
func (s *Store) ListCustomers(ctx context.Context, search string, includeArchived bool, offset, limit int) ([]*model.MultiCustomer, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}

	customers := make([]*model.MultiCustomer, 0)
	q := s.DB.NewSelect().Model(&customers)

	if !includeArchived {
		q = q.Where("archived_at IS NULL")
	}

	search = strings.TrimSpace(search)
	if search != "" {
		normSearch := NormalizePhone(search)
		normEmail := NormalizeEmail(search)
		pattern := "%" + search + "%"

		// License key search detection
		trimmedKey := strings.TrimSpace(search)
		keyHash := license.HashKey(trimmedKey)

		lowerPattern := strings.ToLower(pattern)
		q = q.WhereGroup(" AND ", func(sub *bun.SelectQuery) *bun.SelectQuery {
			sub = sub.Where("LOWER(first_name) LIKE ?", lowerPattern).
				WhereOr("LOWER(last_name) LIKE ?", lowerPattern).
				WhereOr("LOWER(first_name || ' ' || last_name) LIKE ?", lowerPattern).
				WhereOr("LOWER(notes) LIKE ?", lowerPattern)

			if normEmail != "" {
				sub = sub.WhereOr("normalized_email LIKE ?", "%"+normEmail+"%")
			}
			if normSearch != "" {
				sub = sub.WhereOr("normalized_phone LIKE ?", "%"+normSearch+"%")
			}

			// Match via licenses join
			sub = sub.WhereOr("EXISTS (SELECT 1 FROM multi_customer_licenses mcl JOIN licenses l ON l.id = mcl.keygate_license_id WHERE mcl.customer_id = multi_customer.id AND (l.id = ? OR l.key_hash = ? OR l.license_key LIKE ? OR LOWER(l.email) LIKE ?))", trimmedKey, keyHash, "%"+trimmedKey+"%", lowerPattern)
			return sub
		})
	}

	total, err := q.Order("created_at DESC").Offset(offset).Limit(limit).ScanAndCount(ctx)
	if customers == nil {
		customers = make([]*model.MultiCustomer, 0)
	}
	return customers, total, err
}

// ─── License Link & Transfer Operations ───

// AssignLicenseToCustomer assigns an existing KeyGate license to a customer.
func (s *Store) AssignLicenseToCustomer(ctx context.Context, customerID string, licenseID string) (*model.MultiCustomerLicense, error) {
	// Verify customer exists and is not archived
	cust, err := s.FindCustomerByID(ctx, customerID)
	if err != nil {
		return nil, ErrCustomerNotFound
	}

	// Verify license exists
	exists, err := s.DB.NewSelect().Model((*model.License)(nil)).Where("id = ?", licenseID).Exists(ctx)
	if err != nil || !exists {
		return nil, ErrLicenseNotFound
	}

	var assignment *model.MultiCustomerLicense
	err = s.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := time.Now()

		// End any existing active assignment for this license
		_, err := tx.NewUpdate().
			Model((*model.MultiCustomerLicense)(nil)).
			Set("ended_at = ?", now).
			Where("keygate_license_id = ? AND ended_at IS NULL", licenseID).
			Exec(ctx)
		if err != nil {
			return err
		}

		assignment = &model.MultiCustomerLicense{
			ID:               newID(),
			CustomerID:       cust.ID,
			KeygateLicenseID: licenseID,
			AssignedAt:       now,
			CreatedAt:        now,
		}

		if _, err := tx.NewInsert().Model(assignment).Exec(ctx); err != nil {
			return err
		}

		// Record audit event
		ev := &model.MultiCustomerEvent{
			ID:               newID(),
			CustomerID:       cust.ID,
			KeygateLicenseID: licenseID,
			EventType:        model.CRMEventLicenseAssigned,
			Source:           "crm",
			OccurredAt:       now,
			Notes:            fmt.Sprintf("Przypisano istniejącą licencję %s", licenseID),
			CreatedAt:        now,
		}
		if _, err := tx.NewInsert().Model(ev).Exec(ctx); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return assignment, nil
}

// ReassignLicense transfers a license from one customer to another atomically.
func (s *Store) ReassignLicense(ctx context.Context, licenseID string, fromCustomerID string, toCustomerID string) error {
	if _, err := s.FindCustomerByID(ctx, toCustomerID); err != nil {
		return fmt.Errorf("target customer: %w", err)
	}
	if _, err := s.FindCustomerByID(ctx, fromCustomerID); err != nil {
		return fmt.Errorf("source customer: %w", err)
	}

	return s.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		now := time.Now()

		// End previous assignment
		res, err := tx.NewUpdate().
			Model((*model.MultiCustomerLicense)(nil)).
			Set("ended_at = ?", now).
			Where("keygate_license_id = ? AND customer_id = ? AND ended_at IS NULL", licenseID, fromCustomerID).
			Exec(ctx)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return fmt.Errorf("no active assignment found for license %s with customer %s", licenseID, fromCustomerID)
		}

		// Insert new assignment
		assignment := &model.MultiCustomerLicense{
			ID:               newID(),
			CustomerID:       toCustomerID,
			KeygateLicenseID: licenseID,
			AssignedAt:       now,
			CreatedAt:        now,
		}
		if _, err := tx.NewInsert().Model(assignment).Exec(ctx); err != nil {
			return err
		}

		// Record audit event on target customer
		ev := &model.MultiCustomerEvent{
			ID:               newID(),
			CustomerID:       toCustomerID,
			KeygateLicenseID: licenseID,
			EventType:        model.CRMEventLicenseReassigned,
			Source:           "crm",
			OccurredAt:       now,
			Notes:            fmt.Sprintf("Przepisano licencję od klienta %s", fromCustomerID),
			Metadata: map[string]any{
				"from_customer_id": fromCustomerID,
				"to_customer_id":   toCustomerID,
			},
			CreatedAt: now,
		}
		if _, err := tx.NewInsert().Model(ev).Exec(ctx); err != nil {
			return err
		}

		return nil
	})
}

// CreateCRMSaleInTx creates a Keygate license and assigns it to customer in a single atomic transaction.
func (s *Store) CreateCRMSaleInTx(
	ctx context.Context,
	customerID string,
	lic *model.License,
	plan *model.Plan,
	paymentMethod string,
	amount *float64,
	currency string,
	notes string,
) error {
	cust, err := s.FindCustomerByID(ctx, customerID)
	if err != nil {
		return ErrCustomerNotFound
	}

	return s.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		// 1. Create native license using KeyGate core logic
		if err := s.CreateLicenseWithSubscriptionIn(ctx, tx, lic, plan); err != nil {
			return fmt.Errorf("create license: %w", err)
		}

		now := time.Now()
		// 2. Create customer assignment
		assignment := &model.MultiCustomerLicense{
			ID:               newID(),
			CustomerID:       cust.ID,
			KeygateLicenseID: lic.ID,
			AssignedAt:       now,
			CreatedAt:        now,
		}
		if _, err := tx.NewInsert().Model(assignment).Exec(ctx); err != nil {
			return fmt.Errorf("assign license: %w", err)
		}

		// 3. Record license_issued event
		ev := &model.MultiCustomerEvent{
			ID:               newID(),
			CustomerID:       cust.ID,
			KeygateLicenseID: lic.ID,
			EventType:        model.CRMEventLicenseIssued,
			Source:           "crm",
			OccurredAt:       now,
			PeriodFrom:       &lic.CreatedAt,
			PeriodUntil:      lic.ValidUntil,
			PaymentMethod:    paymentMethod,
			Amount:           amount,
			Currency:         currency,
			Notes:            notes,
			CreatedAt:        now,
		}
		if _, err := tx.NewInsert().Model(ev).Exec(ctx); err != nil {
			return fmt.Errorf("record license event: %w", err)
		}

		// 4. Record payment event if amount or payment method provided
		if (amount != nil && *amount > 0) || paymentMethod != "" {
			payEv := &model.MultiCustomerEvent{
				ID:               newID(),
				CustomerID:       cust.ID,
				KeygateLicenseID: lic.ID,
				EventType:        model.CRMEventPaymentRecorded,
				Source:           "crm",
				OccurredAt:       now,
				PaymentMethod:    paymentMethod,
				Amount:           amount,
				Currency:         currency,
				Notes:            notes,
				CreatedAt:        now,
			}
			if _, err := tx.NewInsert().Model(payEv).Exec(ctx); err != nil {
				return fmt.Errorf("record payment event: %w", err)
			}
		}

		return nil
	})
}

// RenewCustomerLicenseInTx renews a license preserving KeyGate invariants.
// For perpetual plans, it applies the renewal through native ApplyLicenseRenewalIn (which updates
// updates_until and writes to license_renewals ledger).
// For subscription/time-limited plans, it extends valid_until and reactivates if expired.
// All updates and CRM events are executed atomically in a transaction.
func (s *Store) RenewCustomerLicenseInTx(
	ctx context.Context,
	customerID string,
	licenseID string,
	durationMonths int,
	customValidUntil *time.Time,
	paymentMethod string,
	amount *float64,
	currency string,
	notes string,
) (*model.License, error) {
	if _, err := s.FindCustomerByID(ctx, customerID); err != nil {
		return nil, ErrCustomerNotFound
	}

	var updatedLic *model.License
	err := s.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		lic := new(model.License)
		q := tx.NewSelect().Model(lic).Where("id = ?", licenseID)
		if !strings.Contains(strings.ToLower(tx.Dialect().Name().String()), "sqlite") {
			q = q.For("UPDATE")
		}
		if err := q.Scan(ctx); err != nil {
			return fmt.Errorf("find license: %w", err)
		}

		// Verify this license is actively assigned to customer
		assigned, err := tx.NewSelect().Model((*model.MultiCustomerLicense)(nil)).
			Where("customer_id = ? AND keygate_license_id = ? AND ended_at IS NULL", customerID, licenseID).
			Exists(ctx)
		if err != nil {
			return err
		}
		if !assigned {
			return fmt.Errorf("license %s is not actively assigned to customer %s", licenseID, customerID)
		}

		plan := new(model.Plan)
		if err := tx.NewSelect().Model(plan).Where("id = ?", lic.PlanID).Scan(ctx); err != nil {
			return fmt.Errorf("find plan: %w", err)
		}

		now := time.Now()
		var periodFrom *time.Time
		var periodUntil *time.Time

		if plan.LicenseType == "perpetual" {
			days := durationMonths * 30
			if durationMonths <= 0 {
				days = 365
			}
			renewal := &model.LicenseRenewal{
				ID:        newID(),
				LicenseID: lic.ID,
				Days:      days,
			}
			if err := ApplyLicenseRenewalIn(ctx, tx, renewal); err != nil {
				return fmt.Errorf("apply perpetual renewal: %w", err)
			}
			periodFrom = renewal.PreviousUpdatesUntil
			periodUntil = renewal.UpdatesUntil
			// Re-read license
			if err := tx.NewSelect().Model(lic).Where("id = ?", lic.ID).Scan(ctx); err != nil {
				return err
			}
		} else {
			// Subscription or time-limited validity license
			var newUntil time.Time
			if customValidUntil != nil {
				newUntil = *customValidUntil
			} else {
				months := durationMonths
				if months <= 0 {
					months = 12
				}
				base := now
				if lic.ValidUntil != nil && lic.ValidUntil.After(now) {
					base = *lic.ValidUntil
				}
				newUntil = base.AddDate(0, months, 0)
			}
			periodFrom = lic.ValidUntil
			periodUntil = &newUntil
			lic.ValidUntil = &newUntil
			if lic.Status == model.StatusExpired || lic.Status == model.StatusSuspended {
				lic.Status = model.StatusActive
				lic.SuspendedAt = nil
			}
			lic.UpdatedAt = now
			if _, err := tx.NewUpdate().Model(lic).
				Column("valid_until", "status", "suspended_at", "updated_at").
				WherePK().Exec(ctx); err != nil {
				return fmt.Errorf("update license validity: %w", err)
			}
		}

		// Record CRM renewal event
		ev := &model.MultiCustomerEvent{
			ID:               newID(),
			CustomerID:       customerID,
			KeygateLicenseID: lic.ID,
			EventType:        model.CRMEventLicenseRenewed,
			Source:           "crm",
			OccurredAt:       now,
			PeriodFrom:       periodFrom,
			PeriodUntil:      periodUntil,
			PaymentMethod:    paymentMethod,
			Amount:           amount,
			Currency:         currency,
			Notes:            notes,
			CreatedAt:        now,
		}
		if _, err := tx.NewInsert().Model(ev).Exec(ctx); err != nil {
			return fmt.Errorf("record renewal event: %w", err)
		}

		// Record payment event if amount or paymentMethod is specified
		if (amount != nil && *amount > 0) || paymentMethod != "" {
			payEv := &model.MultiCustomerEvent{
				ID:               newID(),
				CustomerID:       customerID,
				KeygateLicenseID: lic.ID,
				EventType:        model.CRMEventPaymentRecorded,
				Source:           "crm",
				OccurredAt:       now,
				PaymentMethod:    paymentMethod,
				Amount:           amount,
				Currency:         currency,
				Notes:            notes,
				CreatedAt:        now,
			}
			if _, err := tx.NewInsert().Model(payEv).Exec(ctx); err != nil {
				return fmt.Errorf("record payment event: %w", err)
			}
		}

		updatedLic = lic
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updatedLic, nil
}

// ListCustomerLicenseAssignments lists all license records for a customer.
func (s *Store) ListCustomerLicenseAssignments(ctx context.Context, customerID string) ([]*model.MultiCustomerLicense, error) {
	list := make([]*model.MultiCustomerLicense, 0)
	err := s.DB.NewSelect().
		Model(&list).
		Where("customer_id = ?", customerID).
		Relation("License").
		Relation("License.Product").
		Relation("License.Plan").
		Order("multi_customer_license.created_at DESC").
		Scan(ctx)
	if list == nil {
		list = make([]*model.MultiCustomerLicense, 0)
	}
	return list, err
}

// ─── Event Operations ───

// RecordCustomerEvent saves a CRM event.
func (s *Store) RecordCustomerEvent(ctx context.Context, ev *model.MultiCustomerEvent) error {
	if ev.ID == "" {
		ev.ID = newID()
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now()
	}
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = time.Now()
	}
	if ev.Currency == "" {
		ev.Currency = "PLN"
	}

	_, err := s.DB.NewInsert().Model(ev).Exec(ctx)
	return err
}

// ListCustomerEvents lists events for a customer.
func (s *Store) ListCustomerEvents(ctx context.Context, customerID string) ([]*model.MultiCustomerEvent, error) {
	events := make([]*model.MultiCustomerEvent, 0)
	err := s.DB.NewSelect().
		Model(&events).
		Where("customer_id = ?", customerID).
		Order("occurred_at DESC").
		Scan(ctx)
	if events == nil {
		events = make([]*model.MultiCustomerEvent, 0)
	}
	return events, err
}

// ─── Merge Customers ───

// MergeCustomers merges source customer into target customer, transferring all licenses
// and events while preserving complete history.
func (s *Store) MergeCustomers(ctx context.Context, sourceID, targetID string) error {
	if sourceID == targetID {
		return errors.New("cannot merge customer into themselves")
	}

	source, err := s.FindCustomerByID(ctx, sourceID)
	if err != nil {
		return fmt.Errorf("source customer: %w", err)
	}
	target, err := s.FindCustomerByID(ctx, targetID)
	if err != nil {
		return fmt.Errorf("target customer: %w", err)
	}

	return s.DB.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		now := time.Now()

		// 1. Move all license assignments
		_, err := tx.NewUpdate().
			Model((*model.MultiCustomerLicense)(nil)).
			Set("customer_id = ?", targetID).
			Where("customer_id = ?", sourceID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("move licenses: %w", err)
		}

		// 2. Move all events
		_, err = tx.NewUpdate().
			Model((*model.MultiCustomerEvent)(nil)).
			Set("customer_id = ?", targetID).
			Where("customer_id = ?", sourceID).
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("move events: %w", err)
		}

		// 3. Keep earliest customer_since date
		if source.CustomerSince.Before(target.CustomerSince) {
			target.CustomerSince = source.CustomerSince
		}

		// 4. Merge notes
		if source.Notes != "" {
			if target.Notes != "" {
				target.Notes += "\n---\n[Zmergowano z " + source.FirstName + " " + source.LastName + " (" + source.Phone + ")]:\n" + source.Notes
			} else {
				target.Notes = "[Zmergowano z " + source.FirstName + " " + source.LastName + " (" + source.Phone + ")]:\n" + source.Notes
			}
		}
		target.UpdatedAt = now
		if _, err := tx.NewUpdate().Model(target).Column("customer_since", "notes", "updated_at").WherePK().Exec(ctx); err != nil {
			return fmt.Errorf("update target customer: %w", err)
		}

		// 5. Soft-delete source customer
		source.ArchivedAt = &now
		source.UpdatedAt = now
		if _, err := tx.NewUpdate().Model(source).Column("archived_at", "updated_at").WherePK().Exec(ctx); err != nil {
			return fmt.Errorf("archive source: %w", err)
		}

		// 6. Record merge event on target
		mergeEv := &model.MultiCustomerEvent{
			ID:         newID(),
			CustomerID: targetID,
			EventType:  model.CRMEventCustomerMerged,
			OccurredAt: now,
			Notes:      fmt.Sprintf("Scalono klienta %s %s (%s, ID: %s)", source.FirstName, source.LastName, source.Phone, source.ID),
			Metadata: map[string]any{
				"merged_source_id":    source.ID,
				"merged_source_name":  source.FirstName + " " + source.LastName,
				"merged_source_phone": source.Phone,
			},
			CreatedAt: now,
		}
		if _, err := tx.NewInsert().Model(mergeEv).Exec(ctx); err != nil {
			return fmt.Errorf("record merge event: %w", err)
		}

		return nil
	})
}

// ─── Statistics & Timeline Calculations ───

type timeInterval struct {
	start time.Time
	end   time.Time
}

// CalculateCustomerStatsAndTimeline computes non-overlapping union active time, gaps,
// and chronological timeline according to specifications.
func (s *Store) CalculateCustomerStatsAndTimeline(
	ctx context.Context,
	customer *model.MultiCustomer,
	now time.Time,
) (*model.CustomerStats, []*model.CustomerLicenseItem, []*model.CustomerLicenseItem, []*model.CustomerTimelineItem, error) {
	// Fetch all assignments with joined License, Product, Plan
	assignments, err := s.ListCustomerLicenseAssignments(ctx, customer.ID)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	// Fetch events
	events, err := s.ListCustomerEvents(ctx, customer.ID)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	activeLicenses := make([]*model.CustomerLicenseItem, 0)
	licenseHistory := make([]*model.CustomerLicenseItem, 0)

	currentProducts := make([]string, 0)
	currentPlans := make([]string, 0)
	seenProducts := make(map[string]bool)
	seenPlans := make(map[string]bool)

	var coverageIntervals []timeInterval
	var elapsedIntervals []timeInterval
	var lastPurchaseDate *time.Time
	purchasesCount := 0
	renewalsCount := 0

	for _, a := range assignments {
		lic := a.License
		if lic == nil {
			continue
		}

		// Calculate activations count
		var actCount int
		_ = s.DB.NewRaw("SELECT count(*) FROM activations WHERE license_id = ?", lic.ID).Scan(ctx, &actCount)

		item := &model.CustomerLicenseItem{
			License:          lic,
			LicenseKeyHint:   s.licenseKeyHint(lic),
			AssignedAt:       a.AssignedAt,
			EndedAt:          a.EndedAt,
			IsCurrent:        a.EndedAt == nil && (lic.Status == model.StatusActive || lic.Status == model.StatusTrialing),
			ActivationsCount: actCount,
		}

		licenseHistory = append(licenseHistory, item)

		if item.IsCurrent {
			activeLicenses = append(activeLicenses, item)
			if lic.Product != nil && !seenProducts[lic.Product.Name] {
				seenProducts[lic.Product.Name] = true
				currentProducts = append(currentProducts, lic.Product.Name)
			}
			if lic.Plan != nil && !seenPlans[lic.Plan.Name] {
				seenPlans[lic.Plan.Name] = true
				currentPlans = append(currentPlans, lic.Plan.Name)
			}
		}

		// Coverage interval (used for gaps and overall protection continuity):
		covStart := lic.CreatedAt
		if !lic.ValidFrom.IsZero() {
			covStart = lic.ValidFrom
		}

		var covEnd time.Time
		if lic.ValidUntil != nil {
			covEnd = *lic.ValidUntil
		} else {
			// Perpetual license: active up to now (or when cancelled/suspended)
			covEnd = now
		}

		if lic.CanceledAt != nil && lic.CanceledAt.Before(covEnd) {
			covEnd = *lic.CanceledAt
		}
		if lic.SuspendedAt != nil && lic.SuspendedAt.Before(covEnd) {
			covEnd = *lic.SuspendedAt
		}

		if covEnd.After(covStart) {
			coverageIntervals = append(coverageIntervals, timeInterval{start: covStart, end: covEnd})
		}

		// Elapsed active interval (clamped to now for consumed active duration):
		elapsedEnd := covEnd
		if elapsedEnd.After(now) {
			elapsedEnd = now
		}
		if elapsedEnd.After(covStart) {
			elapsedIntervals = append(elapsedIntervals, timeInterval{start: covStart, end: elapsedEnd})
		}
	}

	// Count purchases and renewals from events
	for _, ev := range events {
		if ev.EventType == model.CRMEventLicenseIssued || (ev.EventType == model.CRMEventPaymentRecorded && (ev.Amount != nil && *ev.Amount > 0)) {
			purchasesCount++
			if lastPurchaseDate == nil || ev.OccurredAt.After(*lastPurchaseDate) {
				d := ev.OccurredAt
				lastPurchaseDate = &d
			}
		}
		if ev.EventType == model.CRMEventLicenseRenewed {
			renewalsCount++
		}
	}

	// Calculate Union of Elapsed Active Intervals
	sort.Slice(elapsedIntervals, func(i, j int) bool {
		return elapsedIntervals[i].start.Before(elapsedIntervals[j].start)
	})

	var mergedElapsed []timeInterval
	for _, iv := range elapsedIntervals {
		if len(mergedElapsed) == 0 {
			mergedElapsed = append(mergedElapsed, iv)
			continue
		}
		last := &mergedElapsed[len(mergedElapsed)-1]
		if iv.start.Before(last.end) || iv.start.Equal(last.end) {
			if iv.end.After(last.end) {
				last.end = iv.end
			}
		} else {
			mergedElapsed = append(mergedElapsed, iv)
		}
	}

	totalActiveSeconds := 0.0
	for _, m := range mergedElapsed {
		totalActiveSeconds += m.end.Sub(m.start).Seconds()
	}
	activeDays := int(totalActiveSeconds / 86400.0)

	// Calculate Gaps between Merged Coverage Intervals
	sort.Slice(coverageIntervals, func(i, j int) bool {
		return coverageIntervals[i].start.Before(coverageIntervals[j].start)
	})

	var mergedCoverage []timeInterval
	for _, iv := range coverageIntervals {
		if len(mergedCoverage) == 0 {
			mergedCoverage = append(mergedCoverage, iv)
			continue
		}
		last := &mergedCoverage[len(mergedCoverage)-1]
		if iv.start.Before(last.end) || iv.start.Equal(last.end) {
			if iv.end.After(last.end) {
				last.end = iv.end
			}
		} else {
			mergedCoverage = append(mergedCoverage, iv)
		}
	}

	gaps := make([]model.CustomerProtectionGap, 0)
	totalGapDays := 0
	for i := 0; i < len(mergedCoverage)-1; i++ {
		gapStart := mergedCoverage[i].end
		gapEnd := mergedCoverage[i+1].start
		diff := gapEnd.Sub(gapStart)
		if diff >= 24*time.Hour {
			days := int(diff.Hours() / 24.0)
			gaps = append(gaps, model.CustomerProtectionGap{
				From: gapStart,
				To:   gapEnd,
				Days: days,
			})
			totalGapDays += days
		}
	}

	stats := &model.CustomerStats{
		CustomerSince:       customer.CustomerSince,
		ActiveLicensesCount: len(activeLicenses),
		TotalLicensesCount:  len(assignments),
		PurchasesCount:      purchasesCount,
		RenewalsCount:       renewalsCount,
		LastPurchaseDate:    lastPurchaseDate,
		CurrentProducts:     currentProducts,
		CurrentPlans:        currentPlans,
		ActiveDays:          activeDays,
		GapsCount:           len(gaps),
		GapDays:             totalGapDays,
		Gaps:                gaps,
	}

	// ─── Build Unified Timeline ───
	timeline := make([]*model.CustomerTimelineItem, 0)

	// 1. Add all events
	for _, ev := range events {
		title := formatEventTitle(ev)
		timeline = append(timeline, &model.CustomerTimelineItem{
			ID:            ev.ID,
			Kind:          "event",
			EventType:     ev.EventType,
			Date:          ev.OccurredAt,
			Title:         title,
			Description:   ev.Notes,
			PeriodFrom:    ev.PeriodFrom,
			PeriodUntil:   ev.PeriodUntil,
			PaymentMethod: ev.PaymentMethod,
			Amount:        ev.Amount,
			Currency:      ev.Currency,
			LicenseID:     ev.KeygateLicenseID,
		})
	}

	// 2. Add gaps to timeline
	for idx, g := range gaps {
		timeline = append(timeline, &model.CustomerTimelineItem{
			ID:          fmt.Sprintf("gap-%d-%d", idx, g.From.Unix()),
			Kind:        "gap",
			Date:        g.From,
			Title:       fmt.Sprintf("Przerwa w ochronie: %d dni", g.Days),
			Description: fmt.Sprintf("Brak aktywnej licencji od %s do %s", g.From.Format("02.01.2006"), g.To.Format("02.01.2006")),
			PeriodFrom:  &g.From,
			PeriodUntil: &g.To,
			GapDays:     g.Days,
		})
	}

	// Enrich timeline items with License / Product / Plan names if available
	licMap := make(map[string]*model.CustomerLicenseItem)
	for _, it := range licenseHistory {
		licMap[it.ID] = it
	}
	for _, item := range timeline {
		if item.LicenseID != "" {
			if lItem, ok := licMap[item.LicenseID]; ok {
				item.LicenseKeyHint = lItem.LicenseKeyHint
				if lItem.Product != nil {
					item.ProductName = lItem.Product.Name
				}
				if lItem.Plan != nil {
					item.PlanName = lItem.Plan.Name
				}
			}
		}
	}

	// Sort timeline chronologically descending
	sort.Slice(timeline, func(i, j int) bool {
		return timeline[i].Date.After(timeline[j].Date)
	})

	if stats.CurrentProducts == nil {
		stats.CurrentProducts = make([]string, 0)
	}
	if stats.CurrentPlans == nil {
		stats.CurrentPlans = make([]string, 0)
	}
	if stats.Gaps == nil {
		stats.Gaps = make([]model.CustomerProtectionGap, 0)
	}
	if activeLicenses == nil {
		activeLicenses = make([]*model.CustomerLicenseItem, 0)
	}
	if licenseHistory == nil {
		licenseHistory = make([]*model.CustomerLicenseItem, 0)
	}
	if timeline == nil {
		timeline = make([]*model.CustomerTimelineItem, 0)
	}

	return stats, activeLicenses, licenseHistory, timeline, nil
}

func (s *Store) licenseKeyHint(l *model.License) string {
	plain := s.DecryptLicenseKey(l)
	if plain == "" {
		return ""
	}
	if len(plain) <= 8 {
		return plain
	}
	return "••••-" + plain[len(plain)-4:]
}

func formatEventTitle(ev *model.MultiCustomerEvent) string {
	switch ev.EventType {
	case model.CRMEventCustomerCreated:
		return "Utworzono kartotekę klienta"
	case model.CRMEventCustomerUpdated:
		return "Zaktualizowano dane klienta"
	case model.CRMEventCustomerArchived:
		return "Zarchiwizowano klienta"
	case model.CRMEventCustomerMerged:
		return "Scalono profil klienta"
	case model.CRMEventLicenseIssued:
		return "Wystawiono nową licencję"
	case model.CRMEventLicenseAssigned:
		return "Przypisano licencję"
	case model.CRMEventLicenseReassigned:
		return "Przeniesiono licencję"
	case model.CRMEventLicenseRenewed:
		return "Przedłużono licencję"
	case model.CRMEventLicenseExpired:
		return "Licencja wygasła"
	case model.CRMEventLicenseCanceled:
		return "Anulowano licencję"
	case model.CRMEventLicenseSuspended:
		return "Zawieszono licencję"
	case model.CRMEventLicenseReactivated:
		return "Odwieszono licencję"
	case model.CRMEventPlanChanged:
		return "Zmieniono plan licencji"
	case model.CRMEventPaymentRecorded:
		return "Zarejestrowano płatność"
	case model.CRMEventRefundRecorded:
		return "Zarejestrowano zwrot płatności"
	case model.CRMEventManualNote:
		return "Notatka administratora"
	default:
		return ev.EventType
	}
}

// LinkStripeLicenseToCRM checks if an existing multi_customer matches the license email.
// Idempotency: Repeated delivery of the same webhook (externalEventID) will not duplicate customer assignments or events.
// Ambiguity / Fail-Closed: If more than 1 customer matches normalized_email, it does NOT pick arbitrarily;
// it leaves the license unassigned and fails closed.
func (s *Store) LinkStripeLicenseToCRM(ctx context.Context, lic *model.License, email string, externalEventID string) error {
	if email == "" || lic == nil {
		return nil
	}
	normEmail := NormalizeEmail(email)
	if normEmail == "" {
		return nil
	}

	// 1. Idempotency check: if external event already recorded, exit cleanly
	if externalEventID != "" {
		exists, err := s.DB.NewSelect().Model((*model.MultiCustomerEvent)(nil)).
			Where("source = ? AND external_event_id = ?", "stripe", externalEventID).
			Exists(ctx)
		if err == nil && exists {
			return nil // already processed, idempotent no-op
		}
	}

	// 2. Fetch matching active customers
	var customers []*model.MultiCustomer
	err := s.DB.NewSelect().Model(&customers).
		Where("normalized_email = ? AND archived_at IS NULL", normEmail).
		Order("created_at ASC").
		Limit(3).
		Scan(ctx)
	if err != nil || len(customers) == 0 {
		return nil // No CRM customer exists for this email
	}

	// 3. Fail closed if ambiguous (multiple customers have this email)
	if len(customers) > 1 {
		slog.Warn("stripe crm link: ambiguous email matches multiple active customers, failing closed",
			"email", normEmail,
			"matching_customers", len(customers),
			"license_id", lic.ID,
		)
		return nil
	}

	cust := customers[0]

	return s.RunInTx(ctx, func(ctx context.Context, tx bun.Tx) error {
		// Check if already assigned
		alreadyAssigned, err := tx.NewSelect().Model((*model.MultiCustomerLicense)(nil)).
			Where("keygate_license_id = ? AND customer_id = ? AND ended_at IS NULL", lic.ID, cust.ID).
			Exists(ctx)
		if err != nil {
			return err
		}

		now := time.Now()
		if !alreadyAssigned {
			assignment := &model.MultiCustomerLicense{
				ID:               newID(),
				CustomerID:       cust.ID,
				KeygateLicenseID: lic.ID,
				AssignedAt:       now,
				CreatedAt:        now,
			}
			if _, err := tx.NewInsert().Model(assignment).Exec(ctx); err != nil {
				return err
			}
		}

		ev := &model.MultiCustomerEvent{
			ID:               newID(),
			CustomerID:       cust.ID,
			KeygateLicenseID: lic.ID,
			EventType:        model.CRMEventLicenseIssued,
			Source:           "stripe",
			ExternalEventID:  externalEventID,
			OccurredAt:       now,
			PeriodFrom:       &lic.CreatedAt,
			PeriodUntil:      lic.ValidUntil,
			PaymentMethod:    model.PaymentMethodStripe,
			Notes:            "Zakup przez Stripe Checkout",
			Metadata: map[string]any{
				"stripe_checkout_session_id": lic.StripeCheckoutSessionID,
				"stripe_payment_intent_id":   lic.StripePaymentIntentID,
				"stripe_subscription_id":     lic.StripeSubscriptionID,
			},
			CreatedAt: now,
		}
		if _, err := tx.NewInsert().Model(ev).Exec(ctx); err != nil {
			return err
		}

		return nil
	})
}

