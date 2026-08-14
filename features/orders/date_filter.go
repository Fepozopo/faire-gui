package orders

import (
	"fmt"
	"strings"
	"time"
)

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
	date, err := time.ParseInLocation("1/2/2006", value, location)
	if err != nil {
		return "", fmt.Errorf("orders: date must use month/day/year: %w", err)
	}
	if endOfDay {
		// Subtract from the next local midnight so daylight-saving transitions retain the user's calendar day.
		date = date.AddDate(0, 0, 1).Add(-time.Second)
	}
	return date.Format(time.RFC3339), nil
}
