// Package ordersstore persists private, connection-scoped Order snapshots and list projections.
//
// It intentionally has no knowledge of Faire HTTP clients, Gio, or application state.
// Callers must provide only validated application-owned storage records and must never
// store credentials, HTTP headers, URLs, or response envelopes.
package ordersstore
