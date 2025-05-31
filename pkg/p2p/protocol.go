// GuiKeyStandaloneGo/pkg/p2p/protocol.go
package p2p

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	// Our shared LogEvent type

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// ProtocolID is the unique string that identifies our log sync protocol on libp2p.
const ProtocolID protocol.ID = "/guikey_standalone/logsync/1.0.0"

// MaxMessageSize prevents OOM from malicious or malformed messages.
const MaxMessageSize = 10 * 1024 * 1024 // 10 MB

// LogBatchRequest is what the client sends to the server.
type LogBatchRequest struct {
	AppClientID         string `json:"app_client_id"`         // The application-level UUID string of the client
	EncryptedLogPayload []byte `json:"encrypted_log_payload"` // JSON marshaled []types.LogEvent, then AES encrypted
}

// LogBatchResponse is what the server sends back to the client.
type LogBatchResponse struct {
	Status          string `json:"status"`           // e.g., "success", "error", "partial_success"
	Message         string `json:"message"`          // Detailed message, especially on error
	EventsProcessed int    `json:"events_processed"` // Number of LogEvent items server claims to have processed from this batch
	ServerTimestamp int64  `json:"server_timestamp"` // Unix timestamp of the server when responding
}

// --- Helper functions for reading/writing messages on a libp2p stream ---

// WriteMessage marshals the given data to JSON, prefixes with length, and writes to the stream.
func WriteMessage(stream network.Stream, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("p2p.WriteMessage: failed to marshal data to JSON: %w", err)
	}

	if len(jsonData) > MaxMessageSize {
		return fmt.Errorf("p2p.WriteMessage: message size %d exceeds MaxMessageSize %d", len(jsonData), MaxMessageSize)
	}

	// Use a buffered writer for efficiency
	writer := bufio.NewWriter(stream)

	// Write length prefix (e.g., 4-byte uint32 big endian) - simple newline for now for easier debugging
	// For production, a binary length prefix is better.
	// For now, let's use newline delimited JSON for easier debugging of the stream.
	// Later we can switch to a binary length prefix.
	_, err = writer.Write(jsonData)
	if err != nil {
		return fmt.Errorf("p2p.WriteMessage: failed to write JSON data: %w", err)
	}
	_, err = writer.WriteString("\n") // Newline delimiter
	if err != nil {
		return fmt.Errorf("p2p.WriteMessage: failed to write newline delimiter: %w", err)
	}

	return writer.Flush()
}

// ReadMessage reads a newline-delimited JSON message from the stream and unmarshals it.
func ReadMessage(stream network.Stream, target interface{}) error {
	// Use a buffered reader
	reader := bufio.NewReader(stream)

	// Read up to newline
	// For production, use a binary length prefix and io.ReadFull or LimitedReader.
	// For now, simple ReadBytes for newline delimited JSON. This is vulnerable to very long lines.
	// We should add a LimitedReader here for safety even with newline.

	// Create a limited reader to prevent reading excessively large lines
	// This is still not as robust as a binary length prefix but better than unlimited ReadBytes.
	lr := io.LimitedReader{R: reader, N: MaxMessageSize + 1} // +1 to detect if it exceeded

	jsonData, err := bufio.NewReader(&lr).ReadBytes('\n')
	if err != nil {
		if err == io.EOF && len(jsonData) > 0 { // EOF but got some data, try to process
			// This can happen if the stream closes without a final newline
		} else if err == io.EOF { // Clean EOF
			return io.EOF // Propagate clean EOF
		}
		return fmt.Errorf("p2p.ReadMessage: failed to read data from stream: %w", err)
	}

	if lr.N == 0 { // We read exactly up to MaxMessageSize + 1, meaning it was too large
		return fmt.Errorf("p2p.ReadMessage: message size exceeded MaxMessageSize %d", MaxMessageSize)
	}

	// Trim the newline before unmarshaling
	jsonData = jsonData[:len(jsonData)-1]

	if err := json.Unmarshal(jsonData, target); err != nil {
		// Log the problematic JSON for debugging (first 200 chars)
		preview := string(jsonData)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return fmt.Errorf("p2p.ReadMessage: failed to unmarshal JSON (preview: '%s'): %w", preview, err)
	}
	return nil
}

// --- Specific message handlers for client & server (optional, could be in their respective p2p_manager files) ---

// Client side: Send a batch and wait for response
func SendLogBatchAndGetResponse(stream network.Stream, clientID string, encryptedPayload []byte) (*LogBatchResponse, error) {
	req := LogBatchRequest{
		AppClientID:         clientID,
		EncryptedLogPayload: encryptedPayload,
	}
	// stream.SetWriteDeadline(time.Now().Add(10 * time.Second)) // Example deadline
	if err := WriteMessage(stream, &req); err != nil {
		return nil, fmt.Errorf("failed to send log batch request: %w", err)
	}

	var resp LogBatchResponse
	// stream.SetReadDeadline(time.Now().Add(30 * time.Second)) // Example deadline
	if err := ReadMessage(stream, &resp); err != nil {
		return nil, fmt.Errorf("failed to read log batch response: %w", err)
	}
	return &resp, nil
}

// Server side: Handle an incoming request (called by stream handler)
// This function would be called by the server's stream handler.
// It takes the stream, decrypts, processes, and then uses WriteMessage to send response.
// For now, this is just a conceptual placeholder of how the server might use ReadMessage.
func HandleIncomingLogBatchRequest(stream network.Stream,

/*
decryptFunc func(data []byte, keyHex string) ([]byte, error),
processFunc func(clientID string, events []types.LogEvent) (int, error),
encryptionKeyHex string,
*/
) (*LogBatchRequest, error) { // Simplified: just reads and returns request for now
	var req LogBatchRequest
	if err := ReadMessage(stream, &req); err != nil {
		return nil, err
	}
	return &req, nil
}
