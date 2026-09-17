package application

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	// sageFulfillmentHTTPAddress is the temporary direct HTTP listener for the Faire GUI workstation.
	sageFulfillmentHTTPAddress = ":18080"
	// sageFulfillmentHTTPPath accepts a Sage fulfillment snapshot and returns one terminal result.
	sageFulfillmentHTTPPath = "/v1/sage/fulfillment"
	// sageFulfillmentHTTPAckPath accepts Sage's post-writeback APPLIED or WRITEBACK_FAILED acknowledgement.
	sageFulfillmentHTTPAckPath = "/v1/sage/fulfillment/ack"
	// sageFulfillmentHTTPAllowedHost is the BSDC01 address allowed by the temporary direct integration.
	sageFulfillmentHTTPAllowedHost = "192.168.128.10"
	// maxSageFulfillmentHTTPBytes prevents an untrusted or malformed request from consuming excessive memory.
	maxSageFulfillmentHTTPBytes = 1024 * 1024
	// sageFulfillmentHTTPWriteTimeout exceeds Sage's five-minute receive timeout to allow its cancellation to reach the handler first.
	sageFulfillmentHTTPWriteTimeout = 6 * time.Minute
)

// serveSageFulfillmentHTTP accepts temporary, firewall-restricted HTTP requests from BSDC01 and publishes validated work to the UI dispatcher.
// ctx ends the listener and any outstanding request when the desktop application shuts down; a non-nil return identifies a listener failure that the UI must display.
func serveSageFulfillmentHTTP(ctx context.Context, publish func(sageFulfillmentInbound), replay func(sageFulfillmentRequest) (sageFulfillmentResult, bool, error), acknowledge func(string, sageWritebackState) error) error {
	listener, err := net.Listen("tcp", sageFulfillmentHTTPAddress)
	if err != nil {
		return err
	}

	server := &http.Server{
		Handler:           http.HandlerFunc(sageFulfillmentHTTPHandler(ctx, publish, sageFulfillmentHTTPCallbacks{replay: replay, acknowledge: acknowledge})),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      sageFulfillmentHTTPWriteTimeout,
		IdleTimeout:       30 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()

	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// sageFulfillmentHTTPCallbacks supplies thread-safe durable replay and acknowledgement operations to the temporary HTTP transport.
type sageFulfillmentHTTPCallbacks struct {
	replay      func(sageFulfillmentRequest) (sageFulfillmentResult, bool, error)
	acknowledge func(string, sageWritebackState) error
}

// sageFulfillmentHTTPAcknowledgement is the narrow post-writeback signal Sage sends after applying or rejecting a completed result.
type sageFulfillmentHTTPAcknowledgement struct {
	ProtocolVersion int                `json:"protocolVersion"`
	RequestID       string             `json:"requestId"`
	Status          sageWritebackState `json:"status"`
}

// sageFulfillmentHTTPHandler validates requests from BSDC01, delegates fulfillment work to the UI goroutine, and returns one typed terminal result.
// The handler deliberately trusts no forwarded-address header because only the TCP peer address is meaningful for the firewall-restricted test endpoint.
func sageFulfillmentHTTPHandler(ctx context.Context, publish func(sageFulfillmentInbound), callbacks ...sageFulfillmentHTTPCallbacks) http.HandlerFunc {
	var callback sageFulfillmentHTTPCallbacks
	if len(callbacks) > 0 {
		callback = callbacks[0]
	}
	return func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || (request.URL.Path != sageFulfillmentHTTPPath && request.URL.Path != sageFulfillmentHTTPAckPath) {
			http.NotFound(response, request)
			return
		}
		remoteHost, _, err := net.SplitHostPort(request.RemoteAddr)
		if err != nil || remoteHost != sageFulfillmentHTTPAllowedHost {
			http.Error(response, "Sage fulfillment endpoint is unavailable.", http.StatusForbidden)
			return
		}

		request.Body = http.MaxBytesReader(response, request.Body, maxSageFulfillmentHTTPBytes)
		payload, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(response, "Sage fulfillment request is too large or unreadable.", http.StatusBadRequest)
			return
		}
		defer request.Body.Close()
		rawPayload := payload
		payload, err = decodeSageFulfillmentHTTPPayload(payload)
		if err != nil {
			http.Error(response, "Sage fulfillment request has an unsupported text encoding.", http.StatusBadRequest)
			return
		}
		if request.URL.Path == sageFulfillmentHTTPAckPath {
			handleSageFulfillmentHTTPAcknowledgement(response, payload, callback)
			return
		}
		fulfillmentRequest, err := parseSageFulfillmentRequest(payload)
		if err != nil {
			// The short hex previews diagnose text encoding without exposing customer or credential data.
			http.Error(response, "Sage fulfillment request is invalid: "+err.Error()+"; raw="+sageFulfillmentHTTPPayloadPreview(rawPayload)+"; normalized="+sageFulfillmentHTTPPayloadPreview(payload), http.StatusBadRequest)
			return
		}

		if callback.replay != nil {
			result, found, replayErr := callback.replay(fulfillmentRequest)
			if replayErr != nil {
				writeSageFulfillmentHTTPResult(response, sageFulfillmentFailure(fulfillmentRequest, "Sage fulfillment request identity could not be verified."))
				return
			}
			if found {
				writeSageFulfillmentHTTPResult(response, result)
				return
			}
		}

		results := make(chan sageFulfillmentResult, 1)
		responder := &sageFulfillmentResponder{send: func(result sageFulfillmentResult) {
			select {
			case results <- result:
			case <-ctx.Done():
			}
		}}
		publish(sageFulfillmentInbound{request: fulfillmentRequest, respond: responder.respond})

		select {
		case result := <-results:
			writeSageFulfillmentHTTPResult(response, result)
		case <-ctx.Done():
			writeSageFulfillmentHTTPResult(response, sageFulfillmentFailure(fulfillmentRequest, "Faire GUI closed before the Sage fulfillment session completed."))
		case <-request.Context().Done():
			// The Sage client has disconnected, so writing would fail; the UI retains its normal session state.
			return
		}
	}
}

// handleSageFulfillmentHTTPAcknowledgement validates and records a narrow post-writeback acknowledgement without involving the Gio frame goroutine.
func handleSageFulfillmentHTTPAcknowledgement(response http.ResponseWriter, payload []byte, callbacks sageFulfillmentHTTPCallbacks) {
	if callbacks.acknowledge == nil {
		http.Error(response, "Sage fulfillment acknowledgement is unavailable.", http.StatusServiceUnavailable)
		return
	}
	var acknowledgement sageFulfillmentHTTPAcknowledgement
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&acknowledgement); err != nil || acknowledgement.ProtocolVersion != sageFulfillmentProtocolVersion || !safeSageRequestID(acknowledgement.RequestID) || (acknowledgement.Status != sageWritebackApplied && acknowledgement.Status != sageWritebackFailed) {
		http.Error(response, "Sage fulfillment acknowledgement is invalid.", http.StatusBadRequest)
		return
	}
	if err := callbacks.acknowledge(acknowledgement.RequestID, acknowledgement.Status); err != nil {
		http.Error(response, "Sage fulfillment acknowledgement could not be recorded.", http.StatusBadRequest)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

// sageFulfillmentHTTPPayloadPreview returns at most the first 16 payload bytes as hexadecimal for temporary encoding diagnostics.
func sageFulfillmentHTTPPayloadPreview(payload []byte) string {
	const previewLength = 16
	if len(payload) > previewLength {
		payload = payload[:previewLength]
	}
	return hex.EncodeToString(payload)
}

// decodeSageFulfillmentHTTPPayload accepts UTF-8 JSON and the UTF-16 BSTR byte orders emitted by ServerXMLHTTP.Send.
// VBScript can omit or choose a byte-order mark, so the initial JSON opening-brace pattern selects the matching decoder.
func decodeSageFulfillmentHTTPPayload(payload []byte) ([]byte, error) {
	if len(payload) >= 3 && payload[0] == 0xEF && payload[1] == 0xBB && payload[2] == 0xBF {
		return payload[3:], nil
	}

	byteOffset := 0
	littleEndian := true
	switch {
	case len(payload) >= 2 && payload[0] == 0xFF && payload[1] == 0xFE:
		byteOffset = 2
	case len(payload) >= 2 && payload[0] == 0xFE && payload[1] == 0xFF:
		byteOffset = 2
		littleEndian = false
	case len(payload) >= 2 && payload[0] == '{' && payload[1] == 0:
		// JSON begins with an ASCII opening brace, making its second UTF-16LE byte NUL.
	case len(payload) >= 2 && payload[0] == 0 && payload[1] == '{':
		littleEndian = false
	default:
		return payload, nil
	}
	if (len(payload)-byteOffset)%2 != 0 {
		return nil, errors.New("UTF-16 payload has an incomplete code unit")
	}
	codeUnits := make([]uint16, 0, (len(payload)-byteOffset)/2)
	for index := byteOffset; index < len(payload); index += 2 {
		if littleEndian {
			codeUnits = append(codeUnits, uint16(payload[index])|uint16(payload[index+1])<<8)
		} else {
			codeUnits = append(codeUnits, uint16(payload[index+1])|uint16(payload[index])<<8)
		}
	}
	return []byte(strings.TrimRight(string(utf16.Decode(codeUnits)), "\x00")), nil
}

// writeSageFulfillmentHTTPResult sends the existing narrow result-line format so Sage continues to apply only whitelisted writeback fields.
func writeSageFulfillmentHTTPResult(response http.ResponseWriter, result sageFulfillmentResult) {
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(response, formatSageFulfillmentResult(result))
}
