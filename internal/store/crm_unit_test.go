package store_test

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tabloy/keygate/internal/model"
	"github.com/tabloy/keygate/internal/store"
)

// ─── Test 1 & 2: Normalization ───

func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"500 123 456", "+48500123456"},
		{"500-123-456", "+48500123456"},
		{"(500) 123 456", "+48500123456"},
		{"+48 500 123 456", "+48500123456"},
		{"+48-500-123-456", "+48500123456"},
		{"0048 500 123 456", "+48500123456"},
		{"+1 (555) 234-5678", "+15552345678"},
		{"  +44 20 7946 0958  ", "+442079460958"},
		{"   ", ""},
		{"", ""},
	}

	for _, tc := range cases {
		actual := store.NormalizePhone(tc.input)
		if actual != tc.expected {
			t.Errorf("NormalizePhone(%q) = %q, expected %q", tc.input, actual, tc.expected)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"Jan.Kowalski@Example.COM", "jan.kowalski@example.com"},
		{"  TEST@DOMAIN.PL  ", "test@domain.pl"},
		{"user+tag@domain.com", "user+tag@domain.com"},
		{"", ""},
	}

	for _, tc := range cases {
		actual := store.NormalizeEmail(tc.input)
		if actual != tc.expected {
			t.Errorf("NormalizeEmail(%q) = %q, expected %q", tc.input, actual, tc.expected)
		}
	}
}

// ─── Test 19, 20: Active Time Union Calculation ───

type interval struct {
	start time.Time
	end   time.Time
}

func calculateActiveDaysUnion(intervals []interval) int {
	if len(intervals) == 0 {
		return 0
	}

	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i].start.Before(intervals[j].start)
	})

	var merged []interval
	for _, iv := range intervals {
		if len(merged) == 0 {
			merged = append(merged, iv)
			continue
		}
		last := &merged[len(merged)-1]
		if iv.start.Before(last.end) || iv.start.Equal(last.end) {
			if iv.end.After(last.end) {
				last.end = iv.end
			}
		} else {
			merged = append(merged, iv)
		}
	}

	totalSeconds := 0.0
	for _, m := range merged {
		totalSeconds += m.end.Sub(m.start).Seconds()
	}
	return int(totalSeconds / 86400.0)
}

func TestActiveTimeUnion_OverlappingLicensesDoNotDouble(t *testing.T) {
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC) // 31 days

	// Client has TWO licenses concurrently for January 2025
	intervals := []interval{
		{start: t1, end: t2},
		{start: t1, end: t2},
	}

	days := calculateActiveDaysUnion(intervals)
	if days != 31 {
		t.Fatalf("expected 31 active days (union), got %d", days)
	}
}

func TestActiveTimeUnion_PartiallyOverlappingLicenses(t *testing.T) {
	t1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)
	t4 := time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC) // full union 01.01 to 31.01 = 30 days

	intervals := []interval{
		{start: t1, end: t2},
		{start: t3, end: t4},
	}

	days := calculateActiveDaysUnion(intervals)
	if days != 30 {
		t.Fatalf("expected 30 active days, got %d", days)
	}
}

// ─── Test 21: Gap Calculation ───

func calculateGaps(intervals []interval) []int {
	if len(intervals) <= 1 {
		return nil
	}
	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i].start.Before(intervals[j].start)
	})

	var merged []interval
	for _, iv := range intervals {
		if len(merged) == 0 {
			merged = append(merged, iv)
			continue
		}
		last := &merged[len(merged)-1]
		if iv.start.Before(last.end) || iv.start.Equal(last.end) {
			if iv.end.After(last.end) {
				last.end = iv.end
			}
		} else {
			merged = append(merged, iv)
		}
	}

	var gaps []int
	for i := 0; i < len(merged)-1; i++ {
		gapStart := merged[i].end
		gapEnd := merged[i+1].start
		if gapEnd.Sub(gapStart) >= 24*time.Hour {
			days := int(gapEnd.Sub(gapStart).Hours() / 24.0)
			gaps = append(gaps, days)
		}
	}
	return gaps
}

func TestGapCalculation(t *testing.T) {
	// License 1: 14.02.2025 -> 14.04.2025 (incl. renewal)
	// Gap: 14.04.2025 -> 10.05.2025 (26 days)
	// License 2: 10.05.2025 -> 10.06.2025
	lic1Start := time.Date(2025, 2, 14, 0, 0, 0, 0, time.UTC)
	lic1End := time.Date(2025, 4, 14, 0, 0, 0, 0, time.UTC)

	lic2Start := time.Date(2025, 5, 10, 0, 0, 0, 0, time.UTC)
	lic2End := time.Date(2025, 6, 10, 0, 0, 0, 0, time.UTC)

	intervals := []interval{
		{start: lic1Start, end: lic1End},
		{start: lic2Start, end: lic2End},
	}

	gaps := calculateGaps(intervals)
	if len(gaps) != 1 {
		t.Fatalf("expected 1 gap, got %d", len(gaps))
	}
	if gaps[0] != 26 {
		t.Fatalf("expected 26 days gap, got %d", gaps[0])
	}
}

// ─── Test 26: Public API Data Leak Prevention ───

func TestPublicLicenseModelDoesNotLeakCRMData(t *testing.T) {
	lic := &model.License{
		ID:        "lic-123",
		ProductID: "prod-456",
		PlanID:    "plan-789",
		Email:     "customer@example.com",
		Status:    model.StatusActive,
	}

	data, err := json.Marshal(lic)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)
	forbiddenKeys := []string{
		"first_name",
		"last_name",
		"phone",
		"normalized_phone",
		"crm_notes",
		"customer_id",
		"payment_method",
		"amount",
		"multi_customer",
	}

	for _, key := range forbiddenKeys {
		if strings.Contains(jsonStr, `"`+key+`"`) {
			t.Fatalf("data leak: model.License JSON contains CRM field %q", key)
		}
	}
}

// ─── Test 10: Deduplication / No Auto-Merge on First + Last Name ───

func TestDeduplicationRequiresExactPhoneOrEmail(t *testing.T) {
	// Two customers with identical names but different phones/emails
	custA := &model.MultiCustomer{
		FirstName:       "Jan",
		LastName:        "Kowalski",
		Phone:           "500 111 222",
		NormalizedPhone: store.NormalizePhone("500 111 222"),
		Email:           "jan.k@example.com",
		NormalizedEmail: store.NormalizeEmail("jan.k@example.com"),
	}

	custB := &model.MultiCustomer{
		FirstName:       "Jan",
		LastName:        "Kowalski",
		Phone:           "600 333 444",
		NormalizedPhone: store.NormalizePhone("600 333 444"),
		Email:           "jan.kowalski.warszawa@example.com",
		NormalizedEmail: store.NormalizeEmail("jan.kowalski.warszawa@example.com"),
	}

	// Must NOT match on phone or email
	if custA.NormalizedPhone == custB.NormalizedPhone {
		t.Fatal("normalized phone should not match")
	}
	if custA.NormalizedEmail == custB.NormalizedEmail {
		t.Fatal("normalized email should not match")
	}
}
