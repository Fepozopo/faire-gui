package orders

import (
	"testing"
	"time"
)

// TestNormalizeDateFilterUsesRequestedLocalDay verifies date-only input becomes the correct inclusive local-day boundary.
func TestNormalizeDateFilterUsesRequestedLocalDay(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("UTC-05", -5*60*60)
	for _, test := range []struct {
		name     string
		value    string
		endOfDay bool
		want     string
	}{
		{name: "start of day", value: "03/21/2026", want: "2026-03-21T00:00:00-05:00"},
		{name: "end of day", value: "3/21/2026", endOfDay: true, want: "2026-03-21T23:59:59-05:00"},
		{name: "blank", value: "  ", want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeDateFilter(test.value, test.endOfDay, location)
			if err != nil {
				t.Fatalf("NormalizeDateFilter() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("NormalizeDateFilter() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestNormalizeDateFilterRejectsInvalidInput verifies malformed calendar dates do not reach Faire as filters.
func TestNormalizeDateFilterRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeDateFilter("02/30/2026", false, time.Local); err == nil {
		t.Fatal("NormalizeDateFilter() error = nil, want invalid-date error")
	}
}
