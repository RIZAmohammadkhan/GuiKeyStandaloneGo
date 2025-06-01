// File: GuiKeyStandaloneGo/generator/templates/server_template/protocol.go
package p2p

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
)

const ProtocolID = "/guikey/logbatch/1.0.0"

func init() {
	rand.Seed(time.Now().UnixNano())
}

type MessageType string

const (
	MessageTypeLogBatch MessageType = "LogBatchRequest"
	MessageTypeError    MessageType = "ErrorResponse"
)

// MessageHeader defines the common fields for all request/response types.
type MessageHeader struct {
	MessageType MessageType `json:"messageType"`
	RequestID   string      `json:"requestID"` // 8-byte hex string
}

// NewMessageHeader creates a new header with the given type and a random RequestID.
func NewMessageHeader(msgType MessageType) MessageHeader {
	return MessageHeader{
		MessageType: msgType,
		RequestID:   GenerateRequestID(),
	}
}

// GenerateRequestID returns a random 8-byte hex string.
func GenerateRequestID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// LogBatchRequest is sent by the client: encrypted payload of log events.
type LogBatchRequest struct {
	Header              MessageHeader `json:"header"`
	AppClientID         string        `json:"appClientID"`
	EncryptedLogPayload []byte        `json:"encryptedLogPayload"`
}

// LogBatchResponse is returned by the server upon success.
type LogBatchResponse struct {
	Header          MessageHeader `json:"header"`
	Status          string        `json:"status"`
	Message         string        `json:"message"`
	EventsProcessed int           `json:"eventsProcessed"`
	EventsRejected  int           `json:"eventsRejected"`
	ServerTimestamp int64         `json:"serverTimestamp"`
}

// ErrorResponse is returned if anything goes wrong.
type ErrorResponse struct {
	Header    MessageHeader `json:"header"`
	ErrorCode string        `json:"errorCode"`
	ErrorMsg  string        `json:"errorMsg"`
	Retryable bool          `json:"retryable"`
}

// writePrefixedJSON marshals `msg` to JSON, prefixes it with a 4-byte length, and writes to `w`.
func writePrefixedJSON(w io.Writer, msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	lengthBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBuf, uint32(len(data)))
	if _, err := w.Write(lengthBuf); err != nil {
		return fmt.Errorf("failed to write length prefix: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("failed to write JSON payload: %w", err)
	}
	return nil
}

// readPrefixedJSON reads a 4-byte length prefix, then reads that many bytes and unmarshals into `msg`.
func readPrefixedJSON(r io.Reader, msg interface{}) error {
	lengthBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lengthBuf); err != nil {
		return fmt.Errorf("failed to read length prefix: %w", err)
	}
	length := binary.BigEndian.Uint32(lengthBuf)
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return fmt.Errorf("failed to read JSON payload: %w", err)
	}
	if err := json.Unmarshal(payload, msg); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	return nil
}

// WriteMessage writes `msg` over the given libp2p stream.
func WriteMessage(stream network.Stream, msg interface{}) error {
	writer := bufio.NewWriter(stream)
	if err := writePrefixedJSON(writer, msg); err != nil {
		return fmt.Errorf("WriteMessage: %w", err)
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("WriteMessage flush: %w", err)
	}
	return nil
}

// WriteMessageWithTimeout wraps WriteMessage with a context‐based timeout.
func WriteMessageWithTimeout(stream network.Stream, msg interface{}, timeout time.Duration) error {
	if err := stream.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("WriteMessageWithTimeout: set write deadline: %w", err)
	}
	return WriteMessage(stream, msg)
}

// ReadMessage reads a prefixed JSON message from the stream into `msg`.
func ReadMessage(stream network.Stream, msg interface{}) error {
	reader := bufio.NewReader(stream)
	return readPrefixedJSON(reader, msg)
}

// ReadMessageWithTimeout wraps ReadMessage with a read deadline.
func ReadMessageWithTimeout(stream network.Stream, msg interface{}, timeout time.Duration) error {
	if err := stream.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("ReadMessageWithTimeout: set read deadline: %w", err)
	}
	return ReadMessage(stream, msg)
}

// SendLogBatchResponse builds and sends a LogBatchResponse, overriding the header’s RequestID.
func SendLogBatchResponse(stream network.Stream, requestID, status, message string, eventsProcessed, eventsRejected int) error {
	resp := LogBatchResponse{
		Header:          NewMessageHeader(MessageTypeLogBatch),
		Status:          status,
		Message:         message,
		EventsProcessed: eventsProcessed,
		EventsRejected:  eventsRejected,
		ServerTimestamp: time.Now().Unix(),
	}
	// Override the randomly generated ID to match the client’s
	resp.Header.RequestID = requestID
	return WriteMessage(stream, &resp)
}

// SendErrorResponse builds and sends an ErrorResponse, overriding the header’s RequestID.
func SendErrorResponse(stream network.Stream, requestID, errorCode, errorMsg string, retryable bool) error {
	resp := ErrorResponse{
		Header:    NewMessageHeader(MessageTypeError),
		ErrorCode: errorCode,
		ErrorMsg:  errorMsg,
		Retryable: retryable,
	}
	resp.Header.RequestID = requestID
	return WriteMessage(stream, &resp)
}
