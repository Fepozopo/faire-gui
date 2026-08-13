package orders

import (
	"errors"
	"strings"
	"unicode"

	"github.com/Fepozopo/faire-gui/faire"
)

// ErrInvalidDisplayID indicates that input is not a canonical Faire display ID.
var ErrInvalidDisplayID = errors.New("invalid Faire order display ID")

// OrderIDFromDisplayID normalizes a display ID with optional surrounding
// whitespace and a leading hash, then converts it to Faire's order-ID form. Only
// one to 128 ASCII letters or digits are accepted to prevent arbitrary text from
// becoming a request identifier while supporting display-ID lengths beyond a single example.
func OrderIDFromDisplayID(displayID string) (faire.OrderID, error) {
	normalized := strings.TrimSpace(displayID)
	if withoutHash, found := strings.CutPrefix(normalized, "#"); found {
		normalized = strings.TrimSpace(withoutHash)
	}
	if len(normalized) == 0 || len(normalized) > 128 {
		return "", ErrInvalidDisplayID
	}
	for _, character := range normalized {
		if !isASCIIAlphaNumeric(character) {
			return "", ErrInvalidDisplayID
		}
	}
	return faire.OrderID("bo_" + strings.ToLower(normalized)), nil
}

// isASCIIAlphaNumeric avoids accepting visually similar Unicode characters in an
// identifier that is later used as part of an API request path.
func isASCIIAlphaNumeric(character rune) bool {
	return character <= unicode.MaxASCII && (character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9')
}
