//go:build windows

package application

import (
	"context"
	"io"
	"net"

	"github.com/Microsoft/go-winio"
)

const (
	// sageFulfillmentInputPipeName is the workstation-local endpoint written by Sage/LaunchFaireFulfillment.vbs.
	sageFulfillmentInputPipeName = `\\.\pipe\FaireGUIFulfillmentIn`
	// sageFulfillmentOutputPipePrefix is combined with a validated request ID so each waiting Sage script receives only its own result.
	sageFulfillmentOutputPipePrefix = `\\.\pipe\FaireGUIFulfillmentOut-`
	// maxSageFulfillmentPipeBytes bounds local request memory before JSON parsing.
	maxSageFulfillmentPipeBytes = 1024 * 1024
)

// serveSageFulfillmentPipes accepts Sage requests until application shutdown and publishes validated work to the UI-owned dispatcher.
func serveSageFulfillmentPipes(ctx context.Context, publish func(sageFulfillmentInbound)) {
	listener, err := winio.ListenPipe(sageFulfillmentInputPipeName, &winio.PipeConfig{MessageMode: true, InputBufferSize: maxSageFulfillmentPipeBytes, OutputBufferSize: 4096})
	if err != nil {
		return
	}
	defer listener.Close()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		go handleSageFulfillmentInput(ctx, connection, publish)
	}
}

// handleSageFulfillmentInput decodes one Sage pipe request, creates its request-specific result pipe before notifying the UI, and reports parser failures through the same result format.
func handleSageFulfillmentInput(ctx context.Context, connection net.Conn, publish func(sageFulfillmentInbound)) {
	defer connection.Close()
	payload, err := io.ReadAll(io.LimitReader(connection, maxSageFulfillmentPipeBytes+1))
	if err != nil || len(payload) > maxSageFulfillmentPipeBytes {
		return
	}
	requestPayload, err := decodeSagePipePayload(payload)
	if err != nil {
		return
	}
	request, err := parseSageFulfillmentRequest(requestPayload)
	if err != nil {
		return
	}

	results := make(chan sageFulfillmentResult, 1)
	responder := &sageFulfillmentResponder{send: func(result sageFulfillmentResult) {
		select {
		case results <- result:
		case <-ctx.Done():
		}
	}}
	if !serveSageFulfillmentResultPipe(ctx, request.RequestID, results) {
		return
	}
	publish(sageFulfillmentInbound{request: request, respond: responder.respond})
}

// serveSageFulfillmentResultPipe opens a per-request response listener before Sage attempts to connect, then writes one terminal response or a cancellation result.
func serveSageFulfillmentResultPipe(ctx context.Context, requestID string, results <-chan sageFulfillmentResult) bool {
	listener, err := winio.ListenPipe(sageFulfillmentOutputPipePrefix+requestID, &winio.PipeConfig{MessageMode: true, InputBufferSize: 4096, OutputBufferSize: 4096})
	if err != nil {
		return false
	}
	go func() {
		defer listener.Close()
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		select {
		case result := <-results:
			_, _ = io.WriteString(connection, formatSageFulfillmentResult(result))
		case <-ctx.Done():
			_, _ = io.WriteString(connection, formatSageFulfillmentResult(sageFulfillmentFailure(sageFulfillmentRequest{RequestID: requestID}, "Faire GUI closed before the Sage fulfillment session completed.")))
		}
	}()
	return true
}
