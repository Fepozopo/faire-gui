// Package orderssync coordinates typed Faire Orders pages with connection-scoped local persistence.
//
// It owns pagination, snapshot conversion, overlap-safe checkpoints, and remote-data
// validation. It has no Gio or application-state dependency.
package orderssync
