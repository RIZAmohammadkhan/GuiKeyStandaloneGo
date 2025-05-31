// GuiKeyStandaloneGo/pkg/types/events.go
package types

import (
	"time"

	"github.com/google/uuid"
)

const CurrentSchemaVersion = 2

type LogEvent struct {
	ID                 uuid.UUID `json:"id"`
	ClientID           uuid.UUID `json:"client_id"`
	Timestamp          time.Time `json:"timestamp"` // Start time of ApplicationActivity
	ApplicationName    string    `json:"application_name"`
	InitialWindowTitle string    `json:"initial_window_title"`
	EventData          EventData `json:"event_data"`
	SchemaVersion      uint32    `json:"schema_version"`
}

type EventData struct {
	Type                string               `json:"type"` // "ApplicationActivity"
	ApplicationActivity *ApplicationActivity `json:"data,omitempty"`
	// Add other event types here if needed in the future
}

type ApplicationActivity struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	TypedText string    `json:"typed_text"`
	// ClipboardActions []ClipboardActivity `json:"clipboard_actions"` // Removed as per request
}

// Helper to create a new ApplicationActivity LogEvent
func NewApplicationActivityEvent(
	clientID uuid.UUID,
	appName string,
	initialTitle string,
	startTime time.Time,
	endTime time.Time,
	typedText string,
) LogEvent {
	return LogEvent{
		ID:                 uuid.New(),
		ClientID:           clientID,
		Timestamp:          startTime, // Main event timestamp is session start
		ApplicationName:    appName,
		InitialWindowTitle: initialTitle,
		EventData: EventData{
			Type: "ApplicationActivity",
			ApplicationActivity: &ApplicationActivity{
				StartTime: startTime,
				EndTime:   endTime,
				TypedText: typedText,
			},
		},
		SchemaVersion: CurrentSchemaVersion,
	}
}
