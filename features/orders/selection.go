package orders

import "github.com/Fepozopo/faire-gui/faire"

// ToggleSelection adds id to the selected-ID map or removes it when already
// selected. Empty IDs are ignored because they cannot safely identify an order.
func (s *State) ToggleSelection(id faire.OrderID) {
	if id == "" {
		return
	}
	if s.SelectedIDs == nil {
		s.SelectedIDs = make(map[faire.OrderID]struct{})
	}
	if _, selected := s.SelectedIDs[id]; selected {
		delete(s.SelectedIDs, id)
		return
	}
	s.SelectedIDs[id] = struct{}{}
}

// SelectVisible adds every identified row to the selected-ID map. It only adds
// values, rather than replacing the map, so selecting another visible page does
// not unexpectedly discard a user's earlier bulk-action selection.
func (s *State) SelectVisible(rows []Row) {
	if s.SelectedIDs == nil {
		s.SelectedIDs = make(map[faire.OrderID]struct{})
	}
	for _, row := range rows {
		if row.ID != "" {
			s.SelectedIDs[row.ID] = struct{}{}
		}
	}
}

// ClearSelection removes every selected order while preserving the initialized
// map for the next interaction and for callers that retain a map reference.
func (s *State) ClearSelection() {
	for id := range s.SelectedIDs {
		delete(s.SelectedIDs, id)
	}
}

// IsSelected reports whether id is currently selected for an orders bulk action.
func (s State) IsSelected(id faire.OrderID) bool {
	_, selected := s.SelectedIDs[id]
	return selected
}
