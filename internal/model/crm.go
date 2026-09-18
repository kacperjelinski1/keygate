package model

import (
	"time"

	"github.com/uptrace/bun"
)

// ─── MultiCustomer ───

type MultiCustomer struct {
	bun.BaseModel `bun:"table:multi_customers"`

	ID              string     `bun:",pk" json:"id"`
	FirstName       string     `bun:",notnull" json:"first_name"`
	LastName        string     `bun:",notnull" json:"last_name"`
	Phone           string     `bun:",notnull" json:"phone"`
	NormalizedPhone string     `bun:",notnull" json:"normalized_phone"`
	Email           string     `json:"email,omitempty"`
	NormalizedEmail string     `json:"normalized_email,omitempty"`
	CustomerSince   time.Time  `bun:",nullzero,default:now()" json:"customer_since"`
	Notes           string     `json:"notes"`
	ArchivedAt      *time.Time `json:"archived_at,omitempty"`
	CreatedAt       time.Time  `bun:",nullzero,default:now()" json:"created_at"`
	UpdatedAt       time.Time  `bun:",nullzero,default:now()" json:"updated_at"`

	// Relational data
	Licenses []*MultiCustomerLicense `bun:"rel:has-many,join:id=customer_id" json:"licenses,omitempty"`
	Events   []*MultiCustomerEvent   `bun:"rel:has-many,join:id=customer_id" json:"events,omitempty"`
}

// ─── MultiCustomerLicense ───

type MultiCustomerLicense struct {
	bun.BaseModel `bun:"table:multi_customer_licenses"`

	ID               string     `bun:",pk" json:"id"`
	CustomerID       string     `bun:",notnull" json:"customer_id"`
	KeygateLicenseID string     `bun:",notnull" json:"keygate_license_id"`
	AssignedAt       time.Time  `bun:",nullzero,default:now()" json:"assigned_at"`
	EndedAt          *time.Time `json:"ended_at,omitempty"`
	CreatedAt        time.Time  `bun:",nullzero,default:now()" json:"created_at"`

	License  *License       `bun:"rel:belongs-to,join:keygate_license_id=id" json:"license,omitempty"`
	Customer *MultiCustomer `bun:"rel:belongs-to,join:customer_id=id" json:"customer,omitempty"`
}

// ─── MultiCustomerEvent ───

type MultiCustomerEvent struct {
	bun.BaseModel `bun:"table:multi_customer_events"`

	ID               string         `bun:",pk" json:"id"`
	CustomerID       string         `bun:",notnull" json:"customer_id"`
	KeygateLicenseID string         `json:"keygate_license_id,omitempty"`
	EventType        string         `bun:",notnull" json:"event_type"`
	Source           string         `bun:",notnull,default:'crm'" json:"source,omitempty"`
	ExternalEventID  string         `bun:",nullzero" json:"external_event_id,omitempty"`
	OccurredAt       time.Time      `bun:",nullzero,default:now()" json:"occurred_at"`
	PeriodFrom       *time.Time     `json:"period_from,omitempty"`
	PeriodUntil      *time.Time     `json:"period_until,omitempty"`
	PaymentMethod    string         `json:"payment_method,omitempty"`
	Amount           *float64       `json:"amount,omitempty"`
	Currency         string         `bun:",notnull,default:'PLN'" json:"currency"`
	Notes            string         `json:"notes,omitempty"`
	Metadata         map[string]any `bun:"type:jsonb" json:"metadata,omitempty"`
	CreatedAt        time.Time      `bun:",nullzero,default:now()" json:"created_at"`
}

const (
	CRMEventCustomerCreated    = "customer_created"
	CRMEventCustomerUpdated    = "customer_updated"
	CRMEventCustomerArchived   = "customer_archived"
	CRMEventCustomerMerged     = "customer_merged"
	CRMEventLicenseIssued      = "license_issued"
	CRMEventLicenseAssigned    = "license_assigned"
	CRMEventLicenseReassigned  = "license_reassigned"
	CRMEventLicenseRenewed     = "license_renewed"
	CRMEventLicenseExpired     = "license_expired"
	CRMEventLicenseCanceled    = "license_canceled"
	CRMEventLicenseSuspended   = "license_suspended"
	CRMEventLicenseReactivated = "license_reactivated"
	CRMEventPlanChanged        = "plan_changed"
	CRMEventPaymentRecorded    = "payment_recorded"
	CRMEventRefundRecorded     = "refund_recorded"
	CRMEventManualNote         = "manual_note"
)

const (
	PaymentMethodCash         = "cash"
	PaymentMethodBankTransfer = "bank_transfer"
	PaymentMethodCardManual   = "card_manual"
	PaymentMethodStripe       = "stripe"
	PaymentMethodPaypal       = "paypal"
	PaymentMethodOther        = "other"
)

// ─── CRM Statistics & DTOs ───

type CustomerProtectionGap struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	Days int       `json:"days"`
}

type CustomerStats struct {
	CustomerSince       time.Time               `json:"customer_since"`
	ActiveLicensesCount int                     `json:"active_licenses_count"`
	TotalLicensesCount  int                     `json:"total_licenses_count"`
	PurchasesCount      int                     `json:"purchases_count"`
	RenewalsCount       int                     `json:"renewals_count"`
	LastPurchaseDate    *time.Time              `json:"last_purchase_date,omitempty"`
	CurrentProducts     []string                `json:"current_products"`
	CurrentPlans        []string                `json:"current_plans"`
	ActiveDays          int                     `json:"active_days"`
	GapsCount           int                     `json:"gaps_count"`
	GapDays             int                     `json:"gap_days"`
	Gaps                []CustomerProtectionGap `json:"gaps"`
}

type CustomerTimelineItem struct {
	ID             string     `json:"id"`
	Kind           string     `json:"kind"` // "event" | "gap"
	EventType      string     `json:"event_type,omitempty"`
	Date           time.Time  `json:"date"`
	Title          string     `json:"title"`
	Description    string     `json:"description,omitempty"`
	PeriodFrom     *time.Time `json:"period_from,omitempty"`
	PeriodUntil    *time.Time `json:"period_until,omitempty"`
	PaymentMethod  string     `json:"payment_method,omitempty"`
	Amount         *float64   `json:"amount,omitempty"`
	Currency       string     `json:"currency,omitempty"`
	LicenseKeyHint string     `json:"license_key_hint,omitempty"`
	LicenseID      string     `json:"license_id,omitempty"`
	ProductName    string     `json:"product_name,omitempty"`
	PlanName       string     `json:"plan_name,omitempty"`
	GapDays        int        `json:"gap_days,omitempty"`
}

type CustomerDetailResponse struct {
	Customer        *MultiCustomer          `json:"customer"`
	Stats           *CustomerStats          `json:"stats"`
	ActiveLicenses  []*CustomerLicenseItem  `json:"active_licenses"`
	LicenseHistory  []*CustomerLicenseItem  `json:"license_history"`
	Timeline        []*CustomerTimelineItem `json:"timeline"`
}

type CustomerLicenseItem struct {
	*License
	LicenseKeyHint string `json:"license_key_hint"`
	AssignedAt     time.Time `json:"assigned_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	IsCurrent      bool       `json:"is_current"`
	ActivationsCount int      `json:"activations_count"`
}

type CheckDuplicateResult struct {
	ExactPhoneMatch []*MultiCustomer `json:"exact_phone_match"`
	ExactEmailMatch []*MultiCustomer `json:"exact_email_match"`
	HasDuplicates   bool             `json:"has_duplicates"`
}
