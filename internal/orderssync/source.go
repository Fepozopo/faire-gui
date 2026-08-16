package orderssync

import (
	"context"

	"github.com/Fepozopo/faire-gui/faire"
)

// Source retrieves one typed, cursor-paginated Faire Orders page.
type Source interface {
	List(context.Context, *faire.OrderListOptions) (*faire.OrderPage, error)
}

// SourceFunc adapts a function to Source for tests and application composition.
type SourceFunc func(context.Context, *faire.OrderListOptions) (*faire.OrderPage, error)

// List retrieves one page through the adapted function.
func (f SourceFunc) List(ctx context.Context, options *faire.OrderListOptions) (*faire.OrderPage, error) {
	return f(ctx, options)
}
