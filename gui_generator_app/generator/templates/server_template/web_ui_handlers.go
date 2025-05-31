// GuiKeyStandaloneGo/generator/templates/server_template/web_ui_handlers.go
package main

import (
	"fmt"
	"html/template" // Use html/template
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/RIZAmohammadkhan/GuiKeyStandaloneGo/gui_generator_app/pkg/types"
)

var (
	templates *template.Template // Global template cache
)

// LoadTemplates parses all HTML templates from the specified directory.
func LoadTemplates(templateDir string, logger *log.Logger) error {
	if templates == nil {
		templates = template.New("") // Create a new template collection
	}

	// Add custom functions if needed here, for example, a function to check if a field exists
	// funcMap := template.FuncMap{
	// 	"hasTypedText": func(ed types.EventData) bool {
	// 		return ed.ApplicationActivity != nil && ed.ApplicationActivity.TypedText != ""
	// 	},
	// }
	// templates = templates.Funcs(funcMap)

	globPath := filepath.Join(templateDir, "*.html")
	if logger != nil { // Check logger before using
		logger.Printf("WebUI: Loading templates from glob: %s", globPath)
	}

	var err error
	// ParseGlob can be called multiple times on the same template collection to add more templates
	templates, err = templates.ParseGlob(globPath)
	if err != nil {
		return fmt.Errorf("webui: failed to parse templates from %s: %w", globPath, err)
	}
	if logger != nil {
		logger.Printf("WebUI: Templates loaded successfully.")
	}
	return nil
}

// Data structures for templates
type LogsViewData struct {
	Events       []DisplayLogEvent
	CurrentPage  uint
	TotalPages   uint
	PageSize     uint
	PrevPage     uint
	NextPage     uint
	TotalEvents  int64
	ServerPeerID string
}

type DisplayLogEvent struct {
	// Embed original LogEvent. Template will access fields like .ApplicationName, .EventData.ApplicationActivity.TypedText
	types.LogEvent
	ClientID_Short  string // Example of a transformed field for display
	SessionStartStr string
	SessionEndStr   string
	LogTimestampStr string
}

type ErrorPageData struct {
	ErrorTitle   string
	ErrorMessage string
}

// WebUIEnv holds dependencies for handlers
type WebUIEnv struct {
	LogStore     *ServerLogStore
	ServerPeerID string
	Logger       *log.Logger
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/logs", http.StatusFound)
}

func (env *WebUIEnv) viewLogsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("page_size")

	page, _ := strconv.ParseUint(pageStr, 10, 32)
	if page == 0 {
		page = 1
	}

	pageSize, _ := strconv.ParseUint(pageSizeStr, 10, 32)
	if pageSize == 0 {
		pageSize = 25
	}
	if pageSize > 100 {
		pageSize = 100
	}

	events, totalEvents, err := env.LogStore.GetLogEventsPaginated(uint(page), uint(pageSize))
	if err != nil {
		env.Logger.Printf("WebUI Error: Failed to get logs from store: %v", err)
		renderErrorTemplate(w, "Database Error", "Could not retrieve logs from the database.", env.Logger)
		return
	}

	displayEvents := make([]DisplayLogEvent, len(events))
	for i, event := range events {
		// Ensure ApplicationActivity is not nil before trying to access its fields
		var sessionStart, sessionEnd string
		if event.EventData.ApplicationActivity != nil {
			sessionStart = event.EventData.ApplicationActivity.StartTime.In(time.Local).Format("15:04:05") // Just time for brevity
			sessionEnd = event.EventData.ApplicationActivity.EndTime.In(time.Local).Format("15:04:05")
		} else {
			sessionStart = "N/A"
			sessionEnd = "N/A"
		}

		displayEvents[i] = DisplayLogEvent{
			LogEvent:        event, // Embed the original event
			ClientID_Short:  event.ClientID.String()[:8] + "...",
			LogTimestampStr: event.Timestamp.In(time.Local).Format("2006-01-02 15:04:05 MST"),
			SessionStartStr: sessionStart,
			SessionEndStr:   sessionEnd,
		}
	}

	totalPages := uint(0)
	if totalEvents > 0 && pageSize > 0 {
		totalPages = uint((totalEvents + int64(pageSize) - 1) / int64(pageSize))
	}
	if totalPages == 0 {
		totalPages = 1
	}

	data := LogsViewData{
		Events:       displayEvents,
		CurrentPage:  uint(page),
		TotalPages:   totalPages,
		PageSize:     uint(pageSize),
		PrevPage:     uint(max(1, int(page-1))),               // Ensure PrevPage is at least 1
		NextPage:     uint(min(int(totalPages), int(page+1))), // Ensure NextPage doesn't exceed TotalPages
		TotalEvents:  totalEvents,
		ServerPeerID: env.ServerPeerID,
	}

	// Ensure templates is not nil (should have been loaded at startup)
	if templates == nil {
		env.Logger.Println("WebUI Error: Templates not loaded!")
		http.Error(w, "Internal Server Error: Templates not loaded", http.StatusInternalServerError)
		return
	}

	err = templates.ExecuteTemplate(w, "logs_view.html", data)
	if err != nil {
		env.Logger.Printf("WebUI Error: Failed to execute logs_view template: %v", err)
		// Don't try to render error_page.html if ExecuteTemplate itself fails badly, just send plain error
		http.Error(w, "Internal Server Error rendering page", http.StatusInternalServerError)
	}
}

func renderErrorTemplate(w http.ResponseWriter, title string, message string, logger *log.Logger) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError) // Or appropriate status code

	if templates == nil { // Fallback if templates aren't loaded
		logger.Printf("WebUI Error: Templates not loaded, cannot render error page. Fallback error: %s - %s", title, message)
		fmt.Fprintf(w, "<h1>Error: %s</h1><p>%s</p><p>Additionally, templates could not be loaded.</p>", title, message)
		return
	}
	data := ErrorPageData{ErrorTitle: title, ErrorMessage: message}
	err := templates.ExecuteTemplate(w, "error_page.html", data)
	if err != nil {
		logger.Printf("WebUI Error: Failed to execute error_page template (original error: %s - %s): %v", title, message, err)
		// Fallback simple text error if error_page.html itself fails
		fmt.Fprintf(w, "<h1>Error: %s</h1><p>%s</p><p>Additionally, the error page itself failed to render.</p>", title, message)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
