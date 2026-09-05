package application

import (
	"sort"
	"strings"

	"github.com/Fepozopo/faire-gui/connections"
)

// sortedConnectionsByLabel returns a copy of connections ordered alphabetically by
// case-insensitive, trimmed label. Connection IDs provide a deterministic order for
// matching labels, while the caller's slice retains its persistence order.
func sortedConnectionsByLabel(connectionsToSort []connections.Connection) []connections.Connection {
	sorted := append([]connections.Connection(nil), connectionsToSort...)
	sort.Slice(sorted, func(left, right int) bool {
		leftLabel := strings.ToLower(strings.TrimSpace(sorted[left].Label))
		rightLabel := strings.ToLower(strings.TrimSpace(sorted[right].Label))
		if leftLabel == rightLabel {
			return sorted[left].ID < sorted[right].ID
		}
		return leftLabel < rightLabel
	})
	return sorted
}
