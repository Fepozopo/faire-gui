//go:build !windows

package application

import "context"

// serveSageFulfillmentPipes is a no-op outside Windows because Sage custom scripts connect through Windows named pipes.
// It waits for cancellation so the application worker lifecycle remains identical in development and production builds.
func serveSageFulfillmentPipes(ctx context.Context, publish func(sageFulfillmentInbound)) {
	<-ctx.Done()
}
