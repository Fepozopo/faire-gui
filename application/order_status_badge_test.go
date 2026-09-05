package application

import (
	"image/color"
	"testing"
)

// TestOrderStatusBadgeColorsFor verifies every supported Orders-table status receives
// its specified fill and unknown statuses retain the neutral fallback treatment.
func TestOrderStatusBadgeColorsFor(t *testing.T) {
	tests := []struct {
		status string
		want   orderStatusBadgeColors
	}{
		{
			status: "New",
			want:   orderStatusBadgeColors{background: color.NRGBA{R: 71, G: 71, B: 71, A: 255}, text: color.NRGBA{R: 255, G: 255, B: 255, A: 255}},
		},
		{
			status: "Processing",
			want:   orderStatusBadgeColors{background: color.NRGBA{R: 246, G: 239, B: 219, A: 255}, text: color.NRGBA{R: 60, G: 60, B: 60, A: 255}},
		},
		{
			status: "Pre-transit",
			want:   orderStatusBadgeColors{background: color.NRGBA{R: 241, G: 241, B: 241, A: 255}, text: color.NRGBA{R: 60, G: 60, B: 60, A: 255}},
		},
		{
			status: "In transit",
			want:   orderStatusBadgeColors{background: color.NRGBA{R: 241, G: 241, B: 241, A: 255}, text: color.NRGBA{R: 60, G: 60, B: 60, A: 255}},
		},
		{
			status: "Delivered",
			want:   orderStatusBadgeColors{background: color.NRGBA{R: 226, G: 240, B: 230, A: 255}, text: color.NRGBA{R: 60, G: 60, B: 60, A: 255}},
		},
		{
			status: "Canceled",
			want:   orderStatusBadgeColors{background: color.NRGBA{R: 242, G: 228, B: 225, A: 255}, text: color.NRGBA{R: 60, G: 60, B: 60, A: 255}},
		},
		{
			status: "Backordered",
			want:   orderStatusBadgeColors{background: color.NRGBA{R: 230, G: 230, B: 230, A: 255}, text: color.NRGBA{R: 60, G: 60, B: 60, A: 255}},
		},
		{
			status: "Pending retailer confirmation",
			want:   orderStatusBadgeColors{background: color.NRGBA{R: 247, G: 240, B: 216, A: 255}, text: color.NRGBA{R: 60, G: 60, B: 60, A: 255}},
		},
		{
			status: "On Hold",
			want:   orderStatusBadgeColors{background: color.NRGBA{R: 241, G: 241, B: 241, A: 255}, text: color.NRGBA{R: 60, G: 60, B: 60, A: 255}},
		},
	}

	for _, test := range tests {
		t.Run(test.status, func(t *testing.T) {
			if got := orderStatusBadgeColorsFor(test.status); got != test.want {
				t.Fatalf("orderStatusBadgeColorsFor(%q) = %#v, want %#v", test.status, got, test.want)
			}
		})
	}
}
