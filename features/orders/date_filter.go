package orders

import (
	"fmt"
	"strings"
	"time"
)

const (
	dateInputLayout              = "1/2/2006"
	defaultUpdatedAtLookbackDays = 90
)

// DefaultUpdatedAtMinimum returns the 90-day lookback as a date-field value and
// its equivalent RFC 3339 start-of-day timestamp in location. now supplies the
// reference time; a nil location uses the local timezone, matching date filters
// submitted from the desktop UI.
func DefaultUpdatedAtMinimum(now time.Time, location *time.Location) (string, string) {
	if location == nil {
		location = time.Local
	}
	date := now.In(location).AddDate(0, 0, -defaultUpdatedAtLookbackDays)
	startOfDay := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, location)
	return date.Format(dateInputLayout), startOfDay.Format(time.RFC3339)
}

// NormalizeDateFilter converts a user-entered month/day/year date to an RFC 3339 timestamp in location.
// When endOfDay is false, the result is the start of the entered day; when true, it is the final second
// of that day. An empty value remains empty so the corresponding Faire filter is omitted.
func NormalizeDateFilter(value string, endOfDay bool, location *time.Location) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if location == nil {
		return "", fmt.Errorf("orders: date-filter location is required")
	}
	date, err := time.ParseInLocation(dateInputLayout, value, location)
	if err != nil {
		return "", fmt.Errorf("orders: date must use month/day/year: %w", err)
	}
	if endOfDay {
		// Subtract from the next local midnight so daylight-saving transitions retain the user's calendar day.
		date = date.AddDate(0, 0, 1).Add(-time.Second)
	}
	return date.Format(time.RFC3339), nil
}
