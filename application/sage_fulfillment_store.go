package application

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// sageFulfillmentStoreVersion permits explicit durable-record migrations.
	sageFulfillmentStoreVersion = 2
	// sageFulfillmentStoreFilename stores the one non-secret Sage recovery record.
	sageFulfillmentStoreFilename = "sage-fulfillment-sessions.json"
	// sageFulfillmentResultRetention limits recovery for a result Sage never confirmed as applied.
	sageFulfillmentResultRetention = 30 * 24 * time.Hour
)

// sageWritebackState identifies whether Sage has acknowledged applying a terminal result.
type sageWritebackState string

const (
	// sageWritebackPending means Faire completed but Sage has not confirmed its writeback outcome.
	sageWritebackPending sageWritebackState = "PENDING"
	// sageWritebackApplied means Sage reported that it applied the returned result.
	sageWritebackApplied sageWritebackState = "APPLIED"
	// sageWritebackFailed means Sage preserved the result after reporting a writeback failure for operator recovery.
	sageWritebackFailed sageWritebackState = "WRITEBACK_FAILED"
)

// sageFulfillmentRecord is the one replaceable owner-only recovery record. It includes the original request so a restarted GUI can replay its result only to the same Sage document.
type sageFulfillmentRecord struct {
	Request           sageFulfillmentRequest `json:"request"`
	State             sageFulfillmentState   `json:"state"`
	CreatedAtUTC      time.Time              `json:"createdAtUtc"`
	UpdatedAtUTC      time.Time              `json:"updatedAtUtc"`
	ExternalShipments []sageExternalShipment `json:"externalShipments,omitempty"`
	TerminalResult    *sageFulfillmentResult `json:"terminalResult,omitempty"`
	WritebackState    sageWritebackState     `json:"writebackState,omitempty"`
}

// sageFulfillmentStoreDocument is the atomic on-disk representation of the current recovery record. Records is decoded only to migrate version-one files; version two writes Current exclusively.
type sageFulfillmentStoreDocument struct {
	Version int                              `json:"version"`
	Current *sageFulfillmentRecord           `json:"current,omitempty"`
	Records map[string]sageFulfillmentRecord `json:"records,omitempty"`
}

// sageFulfillmentStore serializes changes to one durable recovery record. Its mutex also protects HTTP replay and acknowledgement handlers, which run outside the Gio frame goroutine.
type sageFulfillmentStore struct {
	mu      sync.Mutex
	path    string
	current *sageFulfillmentRecord
}

// sageFulfillmentStorePath returns the owner-specific recovery-data location without creating it.
func sageFulfillmentStorePath() (string, error) {
	configDirectory, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDirectory, "faire-gui", sageFulfillmentStoreFilename), nil
}

// loadSageFulfillmentStore opens the default durable Sage-session store and removes expired recovery data.
func loadSageFulfillmentStore() (*sageFulfillmentStore, error) {
	path, err := sageFulfillmentStorePath()
	if err != nil {
		return nil, err
	}
	return loadSageFulfillmentStoreFile(path, time.Now().UTC())
}

// loadSageFulfillmentStoreFile opens path and exists separately so tests can use an isolated store. Version-one history is intentionally compacted to its newest usable record because the current policy retains only one replaceable recovery slot.
func loadSageFulfillmentStoreFile(path string, now time.Time) (*sageFulfillmentStore, error) {
	store := &sageFulfillmentStore{path: path}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open Sage fulfillment recovery data: %w", err)
	}
	defer file.Close()

	var document sageFulfillmentStoreDocument
	if err := json.UnmarshalRead(file, &document); err != nil {
		return nil, fmt.Errorf("decode Sage fulfillment recovery data: %w", err)
	}

	migrated := false
	switch document.Version {
	case 1:
		store.current = newestSageFulfillmentRecord(document.Records)
		migrated = true
	case sageFulfillmentStoreVersion:
		store.current = document.Current
	default:
		return nil, fmt.Errorf("unsupported Sage fulfillment recovery-data version %d", document.Version)
	}
	if migrated || store.pruneLocked(now) {
		if err := store.saveLocked(); err != nil {
			return nil, err
		}
	}
	return store, nil
}

// newestSageFulfillmentRecord selects the most recently updated non-applied version-one record for migration. Older records intentionally become manual-recovery cases under the one-slot policy.
func newestSageFulfillmentRecord(records map[string]sageFulfillmentRecord) *sageFulfillmentRecord {
	var newest *sageFulfillmentRecord
	for _, record := range records {
		if record.WritebackState == sageWritebackApplied {
			continue
		}
		if newest == nil || record.UpdatedAtUTC.After(newest.UpdatedAtUTC) {
			candidate := record
			newest = &candidate
		}
	}
	return newest
}

// begin records a newly accepted request before the GUI begins irreversible Faire work. A request for a different document replaces the prior recovery slot, making that prior result a deliberate manual-recovery case.
func (store *sageFulfillmentStore) begin(request sageFulfillmentRequest, now time.Time) (sageFulfillmentRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.current != nil && store.current.Request.RequestID == request.RequestID {
		if !sameSageDocument(store.current.Request.Document, request.Document) {
			return sageFulfillmentRecord{}, fmt.Errorf("Sage request ID is already associated with another document")
		}
		return *store.current, nil
	}

	record := sageFulfillmentRecord{Request: request, State: sageFulfillmentStateReceived, CreatedAtUTC: now, UpdatedAtUTC: now}
	previous := store.current
	store.current = &record
	if err := store.saveLocked(); err != nil {
		store.current = previous
		return sageFulfillmentRecord{}, err
	}
	return record, nil
}

// updateShipmentPending persists the external-shipment tracking and cost inputs before Faire receives an irreversible submission.
func (store *sageFulfillmentStore) updateShipmentPending(request sageFulfillmentRequest, shipments []sageExternalShipment, now time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.current == nil || store.current.Request.RequestID != request.RequestID || !sameSageDocument(store.current.Request.Document, request.Document) || store.current.TerminalResult != nil || len(shipments) == 0 {
		return fmt.Errorf("Sage fulfillment recovery record cannot persist shipment submission")
	}
	previous := *store.current
	record := previous
	record.State = sageFulfillmentStateShipmentPending
	record.UpdatedAtUTC = now
	record.ExternalShipments = append([]sageExternalShipment(nil), shipments...)
	store.current = &record
	if err := store.saveLocked(); err != nil {
		store.current = &previous
		return err
	}
	return nil
}

// updateState persists a non-terminal transition before the GUI permits the next workflow action.
func (store *sageFulfillmentStore) updateState(request sageFulfillmentRequest, state sageFulfillmentState, now time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.current == nil || store.current.Request.RequestID != request.RequestID || !sameSageDocument(store.current.Request.Document, request.Document) || store.current.TerminalResult != nil {
		return fmt.Errorf("Sage fulfillment recovery record cannot transition state")
	}
	previous := *store.current
	record := previous
	record.State = state
	record.UpdatedAtUTC = now
	store.current = &record
	if err := store.saveLocked(); err != nil {
		store.current = &previous
		return err
	}
	return nil
}

// complete atomically writes a terminal result before the HTTP responder can expose it to Sage.
func (store *sageFulfillmentStore) complete(request sageFulfillmentRequest, state sageFulfillmentState, result sageFulfillmentResult, now time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.current == nil || store.current.Request.RequestID != request.RequestID || !sameSageDocument(store.current.Request.Document, request.Document) {
		return fmt.Errorf("Sage fulfillment recovery record is missing or belongs to another document")
	}
	if store.current.TerminalResult != nil {
		return nil
	}
	previous := *store.current
	record := previous
	record.State = state
	record.UpdatedAtUTC = now
	record.TerminalResult = &result
	record.WritebackState = sageWritebackPending
	store.current = &record
	if err := store.saveLocked(); err != nil {
		store.current = &previous
		return err
	}
	return nil
}

// replay returns the current terminal result for an exact request ID, or rebinds the one unacknowledged completed result to a fresh request for the same Sage document. Cancellation and ordinary failure start a new workflow instead of replaying.
func (store *sageFulfillmentStore) replay(request sageFulfillmentRequest) (sageFulfillmentResult, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.current == nil {
		return sageFulfillmentResult{}, false, nil
	}
	record := *store.current
	if record.Request.RequestID == request.RequestID {
		if !sameSageDocument(record.Request.Document, request.Document) {
			return sageFulfillmentResult{}, false, fmt.Errorf("Sage request ID is already associated with another document")
		}
		if record.TerminalResult != nil {
			return sageResultWithPackageItemTypes(*record.TerminalResult, request), true, nil
		}
		return sageFulfillmentResult{}, false, nil
	}
	if record.TerminalResult == nil || record.TerminalResult.Status != "COMPLETED" || record.WritebackState == sageWritebackApplied || !sameSageDocument(record.Request.Document, request.Document) {
		return sageFulfillmentResult{}, false, nil
	}

	// Sage creates a fresh request ID on retry. Rebind the one slot before returning it so strict request-ID validation and the later acknowledgement both target this retry.
	result := sageResultWithPackageItemTypes(*record.TerminalResult, request)
	result.RequestID = request.RequestID
	// Sage compares its fixed-width identifiers exactly, whereas document matching deliberately normalizes padding.
	result.SalesOrderNo = request.Document.SalesOrderNo
	result.InvoiceNo = request.Document.InvoiceNo
	previous := record
	record.Request = request
	record.TerminalResult = &result
	record.WritebackState = sageWritebackPending
	record.CreatedAtUTC = time.Now().UTC()
	record.UpdatedAtUTC = record.CreatedAtUTC
	store.current = &record
	if err := store.saveLocked(); err != nil {
		store.current = &previous
		return sageFulfillmentResult{}, false, err
	}
	return result, true, nil
}

// acknowledge records Sage's narrow post-writeback acknowledgement. APPLIED clears the only recovery slot, while WRITEBACK_FAILED keeps it for same-document retry until another Sage request replaces it.
func (store *sageFulfillmentStore) acknowledge(requestID string, state sageWritebackState, now time.Time) (sageFulfillmentResult, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.current == nil || store.current.Request.RequestID != requestID || store.current.TerminalResult == nil {
		return sageFulfillmentResult{}, fmt.Errorf("no completed Sage fulfillment result exists for this request")
	}
	if state != sageWritebackApplied && state != sageWritebackFailed {
		return sageFulfillmentResult{}, fmt.Errorf("unsupported Sage writeback acknowledgement")
	}
	result := *store.current.TerminalResult
	previous := store.current
	if state == sageWritebackApplied {
		store.current = nil
	} else {
		record := *store.current
		record.WritebackState = state
		record.UpdatedAtUTC = now
		store.current = &record
	}
	if err := store.saveLocked(); err != nil {
		store.current = previous
		return sageFulfillmentResult{}, err
	}
	return result, nil
}

// pruneLocked removes an expired terminal recovery result. In-progress work remains until a later Sage request deliberately replaces it, allowing a restarted GUI to resume its current workflow.
func (store *sageFulfillmentStore) pruneLocked(now time.Time) bool {
	if store.current == nil || store.current.TerminalResult == nil || !store.current.UpdatedAtUTC.Before(now.Add(-sageFulfillmentResultRetention)) {
		return false
	}
	store.current = nil
	return true
}

// saveLocked atomically replaces the owner-only recovery file. The caller must hold store.mu.
func (store *sageFulfillmentStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return fmt.Errorf("create Sage fulfillment recovery directory: %w", err)
	}
	temporaryFile, err := os.CreateTemp(filepath.Dir(store.path), ".sage-fulfillment-sessions-*.json")
	if err != nil {
		return fmt.Errorf("create temporary Sage fulfillment recovery data: %w", err)
	}
	temporaryPath := temporaryFile.Name()
	defer os.Remove(temporaryPath)
	if err := temporaryFile.Chmod(0o600); err != nil {
		_ = temporaryFile.Close()
		return fmt.Errorf("secure temporary Sage fulfillment recovery data: %w", err)
	}
	if err := json.MarshalWrite(temporaryFile, sageFulfillmentStoreDocument{Version: sageFulfillmentStoreVersion, Current: store.current}); err != nil {
		_ = temporaryFile.Close()
		return fmt.Errorf("encode Sage fulfillment recovery data: %w", err)
	}
	if err := temporaryFile.Sync(); err != nil {
		_ = temporaryFile.Close()
		return fmt.Errorf("sync Sage fulfillment recovery data: %w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		return fmt.Errorf("close Sage fulfillment recovery data: %w", err)
	}
	if err := os.Rename(temporaryPath, store.path); err != nil {
		return fmt.Errorf("replace Sage fulfillment recovery data: %w", err)
	}
	return nil
}

// sageResultWithPackageItemTypes supplies a type for historical results that predate the package-item-type protocol addition. It copies only unambiguous positive shipped-line types from request and otherwise leaves the value empty for Sage to reject safely rather than guess.
func sageResultWithPackageItemTypes(result sageFulfillmentResult, request sageFulfillmentRequest) sageFulfillmentResult {
	if len(result.PackageItems) == 0 {
		return result
	}
	result.PackageItems = append([]sageFulfillmentPackageItem(nil), result.PackageItems...)
	for index := range result.PackageItems {
		item := &result.PackageItems[index]
		if strings.TrimSpace(item.ItemType) != "" {
			continue
		}
		itemType := ""
		found, ambiguous := false, false
		for _, line := range request.Lines {
			if line.QuantityShipped <= 0 || !strings.EqualFold(strings.TrimSpace(line.ItemCode), strings.TrimSpace(item.ItemCode)) || strings.TrimSpace(line.ItemType) == "" {
				continue
			}
			if found && !strings.EqualFold(itemType, line.ItemType) {
				ambiguous = true
				break
			}
			itemType, found = line.ItemType, true
		}
		if found && !ambiguous {
			item.ItemType = itemType
		}
	}
	return result
}

// sameSageDocument compares the immutable writeback identity attached to a request.
func sameSageDocument(left, right sageFulfillmentDocument) bool {
	return strings.EqualFold(strings.TrimSpace(left.SalesOrderNo), strings.TrimSpace(right.SalesOrderNo)) && strings.EqualFold(strings.TrimSpace(left.InvoiceNo), strings.TrimSpace(right.InvoiceNo))
}
