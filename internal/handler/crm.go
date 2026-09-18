package handler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tabloy/keygate/internal/license"
	"github.com/tabloy/keygate/internal/model"
	"github.com/tabloy/keygate/internal/service"
	"github.com/tabloy/keygate/internal/store"
	"github.com/tabloy/keygate/pkg/apperr"
	"github.com/tabloy/keygate/pkg/response"
)

type CRMHandler struct {
	Store   *store.Store
	Email   *service.EmailService
	Webhook *service.WebhookService
}

func NewCRMHandler(s *store.Store, email *service.EmailService, wh *service.WebhookService) *CRMHandler {
	return &CRMHandler{
		Store:   s,
		Email:   email,
		Webhook: wh,
	}
}

// ─── Customer Endpoints ───

// CustomerListItem holds list summary data for customer table.
type CustomerListItem struct {
	*model.MultiCustomer
	ActiveLicensesCount int        `json:"active_licenses_count"`
	CurrentProduct      string     `json:"current_product"`
	CurrentPlan         string     `json:"current_plan"`
	BillingPeriod       string     `json:"billing_period"`
	ValidUntil          *time.Time `json:"valid_until,omitempty"`
	Status              string     `json:"status"`
}

// ListCustomers handles GET /admin/crm/customers
func (h *CRMHandler) ListCustomers(c *gin.Context) {
	search := c.Query("search")
	archived := c.Query("archived") == "true"
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))

	customers, total, err := h.Store.ListCustomers(c, search, archived, offset, limit)
	if err != nil {
		response.Internal(c)
		return
	}

	// Enrich with primary active license info for high-level table view
	items := make([]CustomerListItem, 0, len(customers))
	now := time.Now()

	for _, cust := range customers {
		assignments, _ := h.Store.ListCustomerLicenseAssignments(c, cust.ID)
		var currentLic *model.License
		activeCount := 0

		for _, a := range assignments {
			if a.EndedAt == nil && a.License != nil {
				if a.License.Status == model.StatusActive || a.License.Status == model.StatusTrialing {
					activeCount++
					if currentLic == nil {
						currentLic = a.License
					}
				}
			}
		}

		item := CustomerListItem{
			MultiCustomer:       cust,
			ActiveLicensesCount: activeCount,
			Status:              "no_license",
		}

		if cust.ArchivedAt != nil {
			item.Status = "archived"
		} else if currentLic != nil {
			item.Status = currentLic.Status
			item.ValidUntil = currentLic.ValidUntil
			if currentLic.Product != nil {
				item.CurrentProduct = currentLic.Product.Name
			}
			if currentLic.Plan != nil {
				item.CurrentPlan = currentLic.Plan.Name
				if currentLic.Plan.BillingInterval != "" {
					item.BillingPeriod = currentLic.Plan.BillingInterval
				} else {
					item.BillingPeriod = currentLic.Plan.LicenseType
				}
			}
		} else if len(assignments) > 0 {
			// Had licenses, but none currently active
			lastA := assignments[0]
			if lastA.License != nil {
				item.Status = lastA.License.Status
				item.ValidUntil = lastA.License.ValidUntil
				if lastA.License.Product != nil {
					item.CurrentProduct = lastA.License.Product.Name
				}
				if lastA.License.Plan != nil {
					item.CurrentPlan = lastA.License.Plan.Name
					item.BillingPeriod = lastA.License.Plan.BillingInterval
				}
			}
		}

		_ = now
		items = append(items, item)
	}

	response.OK(c, gin.H{
		"customers": items,
		"total":     total,
		"offset":    offset,
		"limit":     limit,
	})
}

// GetCustomer handles GET /admin/crm/customers/:id
func (h *CRMHandler) GetCustomer(c *gin.Context) {
	id := c.Param("id")
	cust, err := h.Store.FindCustomerByID(c, id)
	if err != nil {
		if errors.Is(err, store.ErrCustomerNotFound) {
			response.NotFound(c, "customer not found")
			return
		}
		response.Internal(c)
		return
	}

	stats, activeLics, licHistory, timeline, err := h.Store.CalculateCustomerStatsAndTimeline(c, cust, time.Now())
	if err != nil {
		response.Internal(c)
		return
	}

	response.OK(c, model.CustomerDetailResponse{
		Customer:       cust,
		Stats:          stats,
		ActiveLicenses: activeLics,
		LicenseHistory: licHistory,
		Timeline:       timeline,
	})
}

// CreateCustomer handles POST /admin/crm/customers
func (h *CRMHandler) CreateCustomer(c *gin.Context) {
	var req struct {
		FirstName     string `json:"first_name" binding:"required"`
		LastName      string `json:"last_name" binding:"required"`
		Phone         string `json:"phone" binding:"required"`
		Email         string `json:"email"`
		CustomerSince string `json:"customer_since"`
		Notes         string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "first_name, last_name, and phone are required")
		return
	}

	cust := &model.MultiCustomer{
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Phone:     req.Phone,
		Email:     req.Email,
		Notes:     req.Notes,
	}

	if req.CustomerSince != "" {
		if t, err := time.Parse(time.RFC3339, req.CustomerSince); err == nil {
			cust.CustomerSince = t
		}
	}

	if err := h.Store.CreateCustomer(c, cust); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	// Record creation event
	_ = h.Store.RecordCustomerEvent(c, &model.MultiCustomerEvent{
		CustomerID: cust.ID,
		EventType:  model.CRMEventCustomerCreated,
		OccurredAt: cust.CreatedAt,
		Notes:      "Utworzono nowego klienta w systemie CRM",
	})

	h.Store.Audit(c, &model.AuditLog{
		Entity:    "multi_customer",
		EntityID:  cust.ID,
		Action:    "created",
		ActorType: "admin",
		ActorID:   adminID(c),
		Changes: map[string]any{
			"first_name": cust.FirstName,
			"last_name":  cust.LastName,
			"phone":      cust.Phone,
			"email":      cust.Email,
		},
	})

	response.Created(c, cust)
}

// UpdateCustomer handles PUT /admin/crm/customers/:id
func (h *CRMHandler) UpdateCustomer(c *gin.Context) {
	id := c.Param("id")
	cust, err := h.Store.FindCustomerByID(c, id)
	if err != nil {
		if errors.Is(err, store.ErrCustomerNotFound) {
			response.NotFound(c, "customer not found")
			return
		}
		response.Internal(c)
		return
	}

	var req struct {
		FirstName     string `json:"first_name"`
		LastName      string `json:"last_name"`
		Phone         string `json:"phone"`
		Email         string `json:"email"`
		CustomerSince string `json:"customer_since"`
		Notes         string `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}

	if req.FirstName != "" {
		cust.FirstName = req.FirstName
	}
	if req.LastName != "" {
		cust.LastName = req.LastName
	}
	if req.Phone != "" {
		cust.Phone = req.Phone
	}
	cust.Email = req.Email
	cust.Notes = req.Notes

	if req.CustomerSince != "" {
		if t, err := time.Parse(time.RFC3339, req.CustomerSince); err == nil {
			cust.CustomerSince = t
		}
	}

	if err := h.Store.UpdateCustomer(c, cust); err != nil {
		response.Internal(c)
		return
	}

	_ = h.Store.RecordCustomerEvent(c, &model.MultiCustomerEvent{
		CustomerID: cust.ID,
		EventType:  model.CRMEventCustomerUpdated,
		OccurredAt: time.Now(),
		Notes:      "Zaktualizowano dane teleadresowe klienta",
	})

	h.Store.Audit(c, &model.AuditLog{
		Entity:    "multi_customer",
		EntityID:  cust.ID,
		Action:    "updated",
		ActorType: "admin",
		ActorID:   adminID(c),
		Changes: map[string]any{
			"first_name": cust.FirstName,
			"last_name":  cust.LastName,
			"phone":      cust.Phone,
			"email":      cust.Email,
		},
	})

	response.OK(c, cust)
}

// ArchiveCustomer handles DELETE /admin/crm/customers/:id
func (h *CRMHandler) ArchiveCustomer(c *gin.Context) {
	id := c.Param("id")
	if err := h.Store.ArchiveCustomer(c, id); err != nil {
		if errors.Is(err, store.ErrCustomerNotFound) {
			response.NotFound(c, "customer not found or already archived")
			return
		}
		response.Internal(c)
		return
	}

	_ = h.Store.RecordCustomerEvent(c, &model.MultiCustomerEvent{
		CustomerID: id,
		EventType:  model.CRMEventCustomerArchived,
		OccurredAt: time.Now(),
		Notes:      "Zarchiwizowano klienta (soft-delete). Przypisane licencje zachowane.",
	})

	h.Store.Audit(c, &model.AuditLog{
		Entity:    "multi_customer",
		EntityID:  id,
		Action:    "archived",
		ActorType: "admin",
		ActorID:   adminID(c),
	})

	response.OK(c, gin.H{"status": "archived"})
}

// CheckDuplicates handles POST /admin/crm/customers/check-duplicate
func (h *CRMHandler) CheckDuplicates(c *gin.Context) {
	var req struct {
		Phone     string `json:"phone"`
		Email     string `json:"email"`
		ExcludeID string `json:"exclude_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request")
		return
	}

	res, err := h.Store.CheckCustomerDuplicates(c, req.Phone, req.Email, req.ExcludeID)
	if err != nil {
		response.Internal(c)
		return
	}

	response.OK(c, res)
}

// MergeCustomers handles POST /admin/crm/customers/merge
func (h *CRMHandler) MergeCustomers(c *gin.Context) {
	var req struct {
		SourceCustomerID string `json:"source_customer_id" binding:"required"`
		TargetCustomerID string `json:"target_customer_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "source_customer_id and target_customer_id are required")
		return
	}

	if err := h.Store.MergeCustomers(c, req.SourceCustomerID, req.TargetCustomerID); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	h.Store.Audit(c, &model.AuditLog{
		Entity:    "multi_customer",
		EntityID:  req.TargetCustomerID,
		Action:    "merged",
		ActorType: "admin",
		ActorID:   adminID(c),
		Changes: map[string]any{
			"source_customer_id": req.SourceCustomerID,
		},
	})

	response.OK(c, gin.H{"status": "merged"})
}

// ─── Sales & Renewal Workflows ───

// CreateSale handles POST /admin/crm/customers/:id/sale
// Issues a license through KeyGate's core engine, assigns it to the customer,
// records payment and license events, and returns the license with revealed key to admin.
func (h *CRMHandler) CreateSale(c *gin.Context) {
	customerID := c.Param("id")
	cust, err := h.Store.FindCustomerByID(c, customerID)
	if err != nil {
		if errors.Is(err, store.ErrCustomerNotFound) {
			response.NotFound(c, "customer not found")
			return
		}
		response.Internal(c)
		return
	}

	var req struct {
		ProductID     string   `json:"product_id" binding:"required"`
		PlanID        string   `json:"plan_id" binding:"required"`
		Email         string   `json:"email"`
		Notes         string   `json:"notes"`
		ValidUntil    string   `json:"valid_until"`
		PaymentMethod string   `json:"payment_method"` // cash, bank_transfer, card_manual, stripe, paypal, other
		Amount        *float64 `json:"amount"`
		Currency      string   `json:"currency"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "product_id and plan_id are required")
		return
	}

	email := strings.TrimSpace(req.Email)
	if email == "" {
		email = cust.Email
	}
	if email == "" {
		// If neither customer nor request provided an email, generate a deterministic contact placeholder
		// to satisfy KeyGate's internal license email constraint while preserving actual phone as identifier.
		email = "klient-" + cust.NormalizedPhone + "@crm.multi-servis.pl"
	}
	if appErr := apperr.ValidateEmail(email); appErr != nil {
		response.BadRequest(c, appErr.Message)
		return
	}

	plan, err := h.Store.FindPlanByID(c, req.PlanID)
	if err != nil {
		response.NotFound(c, "plan not found")
		return
	}
	if plan.ProductID != req.ProductID {
		response.BadRequest(c, "plan does not belong to the requested product")
		return
	}

	var validUntil *time.Time
	if req.ValidUntil != "" {
		ts, err := time.Parse(time.RFC3339, req.ValidUntil)
		if err != nil {
			response.BadRequest(c, "valid_until must be an RFC 3339 timestamp")
			return
		}
		validUntil = &ts
	} else if plan.BillingInterval == "month" {
		until := time.Now().AddDate(0, 1, 0)
		validUntil = &until
	} else if plan.BillingInterval == "year" {
		until := time.Now().AddDate(1, 0, 0)
		validUntil = &until
	} else if plan.LicenseType == "trial" && plan.TrialDays > 0 {
		until := time.Now().Add(time.Duration(plan.TrialDays) * 24 * time.Hour)
		validUntil = &until
	}

	status := model.StatusActive
	if plan.LicenseType == "trial" {
		status = model.StatusTrialing
	}

	l := &model.License{
		ProductID:  req.ProductID,
		PlanID:     req.PlanID,
		Email:      email,
		LicenseKey: license.GenerateKey(""),
		Status:     status,
		Notes:      req.Notes,
		ValidUntil: validUntil,
	}

	if updates := plan.InitialUpdatesUntil(time.Now()); updates != nil {
		l.UpdatesUntil = updates
	}
	l.UpdatesTermsSet = true

	paymentMethod := req.PaymentMethod
	if paymentMethod == "" {
		paymentMethod = model.PaymentMethodCash
	}
	currency := req.Currency
	if currency == "" {
		currency = "PLN"
	}

	notes := req.Notes
	if notes == "" {
		notes = fmt.Sprintf("Zakup licencji %s - %s", plan.Name, paymentMethod)
	}

	// 1. Create License and assign to customer atomically
	if err := h.Store.CreateCRMSaleInTx(c, cust.ID, l, plan, paymentMethod, req.Amount, currency, notes); err != nil {
		response.Internal(c)
		return
	}

	// 4. Audit
	h.Store.Audit(c, &model.AuditLog{
		Entity:    "multi_customer_sale",
		EntityID:  l.ID,
		Action:    "sale_completed",
		ActorType: "admin",
		ActorID:   adminID(c),
		Changes: map[string]any{
			"customer_id":    cust.ID,
			"license_id":     l.ID,
			"plan_id":        req.PlanID,
			"payment_method": paymentMethod,
			"amount":         req.Amount,
		},
	})

	plainKey := h.Store.DecryptLicenseKey(l)
	c.Header("Cache-Control", "no-store")
	response.Created(c, gin.H{
		"license":     l,
		"license_key": plainKey,
		"customer_id": cust.ID,
	})
}

// RenewLicense handles POST /admin/crm/customers/:id/licenses/:lic_id/renew
func (h *CRMHandler) RenewLicense(c *gin.Context) {
	customerID := c.Param("id")
	licenseID := c.Param("lic_id")

	cust, err := h.Store.FindCustomerByID(c, customerID)
	if err != nil {
		response.NotFound(c, "customer not found")
		return
	}

	lic, err := h.Store.FindLicenseByID(c, licenseID)
	if err != nil {
		response.NotFound(c, "license not found")
		return
	}

	var req struct {
		DurationMonths int      `json:"duration_months"` // e.g. 1 or 12
		CustomValidUntil string `json:"custom_valid_until"`
		PaymentMethod  string   `json:"payment_method"`
		Amount         *float64 `json:"amount"`
		Currency       string   `json:"currency"`
		Notes          string   `json:"notes"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}

	var customUntil *time.Time
	if req.CustomValidUntil != "" {
		ts, err := time.Parse(time.RFC3339, req.CustomValidUntil)
		if err != nil {
			response.BadRequest(c, "custom_valid_until must be an RFC 3339 timestamp")
			return
		}
		customUntil = &ts
	}

	paymentMethod := req.PaymentMethod
	if paymentMethod == "" {
		paymentMethod = model.PaymentMethodCash
	}
	currency := req.Currency
	if currency == "" {
		currency = "PLN"
	}

	updatedLic, err := h.Store.RenewCustomerLicenseInTx(
		c,
		cust.ID,
		lic.ID,
		req.DurationMonths,
		customUntil,
		paymentMethod,
		req.Amount,
		currency,
		req.Notes,
	)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	h.Store.Audit(c, &model.AuditLog{
		Entity:    "license",
		EntityID:  lic.ID,
		Action:    "renewed",
		ActorType: "admin",
		ActorID:   adminID(c),
		Changes: map[string]any{
			"customer_id":     cust.ID,
			"valid_until":     updatedLic.ValidUntil,
			"updates_until":   updatedLic.UpdatesUntil,
			"amount":          req.Amount,
			"duration_months": req.DurationMonths,
		},
	})

	response.OK(c, gin.H{
		"status":        "renewed",
		"license":       updatedLic,
		"valid_until":   updatedLic.ValidUntil,
		"updates_until": updatedLic.UpdatesUntil,
	})
}

// AssignExistingLicense handles POST /admin/crm/customers/:id/licenses/assign
func (h *CRMHandler) AssignExistingLicense(c *gin.Context) {
	customerID := c.Param("id")
	var req struct {
		LicenseID string `json:"license_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "license_id is required")
		return
	}

	assignment, err := h.Store.AssignLicenseToCustomer(c, customerID, req.LicenseID)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	h.Store.Audit(c, &model.AuditLog{
		Entity:    "multi_customer_license",
		EntityID:  assignment.ID,
		Action:    "assigned",
		ActorType: "admin",
		ActorID:   adminID(c),
		Changes: map[string]any{
			"customer_id": customerID,
			"license_id":  req.LicenseID,
		},
	})

	response.OK(c, assignment)
}

// ReassignLicense handles POST /admin/crm/licenses/reassign
func (h *CRMHandler) ReassignLicense(c *gin.Context) {
	var req struct {
		LicenseID      string `json:"license_id" binding:"required"`
		FromCustomerID string `json:"from_customer_id" binding:"required"`
		ToCustomerID   string `json:"to_customer_id" binding:"required"`
		Reason         string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "license_id, from_customer_id, and to_customer_id are required")
		return
	}

	if err := h.Store.ReassignLicense(c, req.LicenseID, req.FromCustomerID, req.ToCustomerID); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	now := time.Now()
	_ = h.Store.RecordCustomerEvent(c, &model.MultiCustomerEvent{
		CustomerID:       req.FromCustomerID,
		KeygateLicenseID: req.LicenseID,
		EventType:        model.CRMEventLicenseReassigned,
		OccurredAt:       now,
		Notes:            fmt.Sprintf("Przeniesiono licencję do klienta ID: %s. Powód: %s", req.ToCustomerID, req.Reason),
	})

	_ = h.Store.RecordCustomerEvent(c, &model.MultiCustomerEvent{
		CustomerID:       req.ToCustomerID,
		KeygateLicenseID: req.LicenseID,
		EventType:        model.CRMEventLicenseAssigned,
		OccurredAt:       now,
		Notes:            fmt.Sprintf("Otrzymano przeniesioną licencję od klienta ID: %s. Powód: %s", req.FromCustomerID, req.Reason),
	})

	h.Store.Audit(c, &model.AuditLog{
		Entity:    "multi_customer_license",
		EntityID:  req.LicenseID,
		Action:    "reassigned",
		ActorType: "admin",
		ActorID:   adminID(c),
		Changes: map[string]any{
			"from_customer_id": req.FromCustomerID,
			"to_customer_id":   req.ToCustomerID,
			"reason":           req.Reason,
		},
	})

	response.OK(c, gin.H{"status": "reassigned"})
}

// RecordCustomerEventManual handles POST /admin/crm/customers/:id/events
func (h *CRMHandler) RecordCustomerEventManual(c *gin.Context) {
	customerID := c.Param("id")
	if _, err := h.Store.FindCustomerByID(c, customerID); err != nil {
		response.NotFound(c, "customer not found")
		return
	}

	var req struct {
		EventType        string   `json:"event_type" binding:"required"`
		KeygateLicenseID string   `json:"keygate_license_id"`
		PaymentMethod    string   `json:"payment_method"`
		Amount           *float64 `json:"amount"`
		Currency         string   `json:"currency"`
		Notes            string   `json:"notes"`
		PeriodFrom       string   `json:"period_from"`
		PeriodUntil      string   `json:"period_until"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "event_type is required")
		return
	}

	ev := &model.MultiCustomerEvent{
		CustomerID:       customerID,
		KeygateLicenseID: req.KeygateLicenseID,
		EventType:        req.EventType,
		OccurredAt:       time.Now(),
		PaymentMethod:    req.PaymentMethod,
		Amount:           req.Amount,
		Currency:         req.Currency,
		Notes:            req.Notes,
	}

	if req.PeriodFrom != "" {
		if t, err := time.Parse(time.RFC3339, req.PeriodFrom); err == nil {
			ev.PeriodFrom = &t
		}
	}
	if req.PeriodUntil != "" {
		if t, err := time.Parse(time.RFC3339, req.PeriodUntil); err == nil {
			ev.PeriodUntil = &t
		}
	}

	if err := h.Store.RecordCustomerEvent(c, ev); err != nil {
		response.Internal(c)
		return
	}

	h.Store.Audit(c, &model.AuditLog{
		Entity:    "multi_customer_event",
		EntityID:  ev.ID,
		Action:    "event_recorded",
		ActorType: "admin",
		ActorID:   adminID(c),
		Changes: map[string]any{
			"customer_id": customerID,
			"event_type":  req.EventType,
		},
	})

	response.Created(c, ev)
}
