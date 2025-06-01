// GuiKeyStandaloneGo/pkg/config/models.go
package config

// For client_settings.toml (used by generator to structure data for client's embedded config)
type ClientSettings struct {
	ServerPeerID                       string   `toml:"server_peer_id"`
	EncryptionKeyHex                   string   `toml:"encryption_key_hex"`
	BootstrapAddresses                 []string `toml:"bootstrap_addresses"`
	ClientID                           string   `toml:"client_id"`
	SyncIntervalSecs                   uint64   `toml:"sync_interval"`
	ProcessorPeriodicFlushIntervalSecs uint64   `toml:"processor_periodic_flush_interval_secs"`
	InternalLogLevel                   string   `toml:"internal_log_level"`
	LogFilePath                        string   `toml:"log_file_path"` // For client's SQLite DB
	AppNameForAutorun                  string   `toml:"app_name_for_autorun"`
	LocalLogCacheRetentionDays         uint32   `toml:"local_log_cache_retention_days"` // For SQLite DB
	RetryIntervalOnFailSecs            uint64   `toml:"retry_interval_on_fail"`
	MaxRetriesPerBatch                 uint32   `toml:"max_retries_per_batch"`
	MaxLogFileSizeMB                   *uint64  `toml:"max_log_file_size_mb"` // For diagnostic text log rotation AND SQLite DB (see note)
	MaxEventsPerSyncBatch              int      `toml:"max_events_per_sync_batch"`
	InternalLogFileDir                 string   `toml:"internal_log_file_dir"`                 // For client's diagnostic logs
	InternalLogFileName                string   `toml:"internal_log_file_name"`                // For client's diagnostic logs
	MaxDiagnosticLogBackups            *int     `toml:"max_diagnostic_log_backups,omitempty"`  // For text log rotation
	MaxDiagnosticLogAgeDays            *int     `toml:"max_diagnostic_log_age_days,omitempty"` // For text log rotation
}

func DefaultClientSettings() ClientSettings {
	defaultSizeMB := uint64(10) // Default for diagnostic text log AND SQLite (can be overridden)
	defaultBackups := 3
	defaultAgeDays := 7

	return ClientSettings{
		ServerPeerID:     "",
		EncryptionKeyHex: "",
		BootstrapAddresses: []string{
			"/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN",
			"/dnsaddr/bootstrap.libp2p.io/p2p/QmQCU2EcMqAqQPR2i9bChDtGNJchTbq5TbXJJ16u19uLTa",
			"/dnsaddr/va1.bootstrap.libp2p.io/p2p/12D3KooWKnDdG3iXw9eTFijk3EWSunZcFi54Zka4wmtqtt6rPxc8",
			"/ip4/104.131.131.82/tcp/4001/p2p/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ",
		},
		ClientID:                           "",
		SyncIntervalSecs:                   60,
		ProcessorPeriodicFlushIntervalSecs: 120,
		InternalLogLevel:                   "info",
		LogFilePath:                        "activity_cache.sqlite",
		AppNameForAutorun:                  "SystemActivityAgent",
		LocalLogCacheRetentionDays:         7,
		RetryIntervalOnFailSecs:            60,
		MaxRetriesPerBatch:                 3,
		MaxLogFileSizeMB:                   &defaultSizeMB,
		MaxEventsPerSyncBatch:              200,
		InternalLogFileDir:                 "client_logs",
		InternalLogFileName:                "monitor_client_diag.log",
		MaxDiagnosticLogBackups:            &defaultBackups,
		MaxDiagnosticLogAgeDays:            &defaultAgeDays,
	}
}

// Methods to get values or defaults for README and templates
func (cs ClientSettings) GetMaxLogFileSizeMBOrDefault() uint64 {
	if cs.MaxLogFileSizeMB != nil {
		return *cs.MaxLogFileSizeMB
	}
	def := DefaultClientSettings() // Call the constructor for defaults
	if def.MaxLogFileSizeMB != nil {
		return *def.MaxLogFileSizeMB
	}
	return 10 // Absolute fallback
}

func (cs ClientSettings) GetMaxDiagnosticLogBackupsOrDefault() int {
	if cs.MaxDiagnosticLogBackups != nil {
		return *cs.MaxDiagnosticLogBackups
	}
	def := DefaultClientSettings()
	if def.MaxDiagnosticLogBackups != nil {
		return *def.MaxDiagnosticLogBackups
	}
	return 3 // Absolute fallback
}

func (cs ClientSettings) GetMaxDiagnosticLogAgeDaysOrDefault() int {
	if cs.MaxDiagnosticLogAgeDays != nil {
		return *cs.MaxDiagnosticLogAgeDays
	}
	def := DefaultClientSettings()
	if def.MaxDiagnosticLogAgeDays != nil {
		return *def.MaxDiagnosticLogAgeDays
	}
	return 7 // Absolute fallback
}

// For local_server_config.toml (used by generator to structure data for server's embedded config)
type ServerSettings struct {
	ListenAddress               string   `toml:"listen_address"`
	WebUIListenAddress          string   `toml:"web_ui_listen_address"`
	EncryptionKeyHex            string   `toml:"encryption_key_hex"`
	ServerIdentityKeySeedHex    string   `toml:"server_identity_key_seed_hex"`
	DatabasePath                string   `toml:"database_path"`
	LogRetentionDays            uint32   `toml:"log_retention_days"`
	LogDeletionCheckIntervalHrs uint64   `toml:"log_deletion_check_interval_hours"`
	BootstrapAddresses          []string `toml:"bootstrap_addresses,omitempty"`
	InternalLogLevel            string   `toml:"internal_log_level,omitempty"`
	InternalLogFileDir          string   `toml:"internal_log_file_dir,omitempty"`
	InternalLogFileName         string   `toml:"internal_log_file_name,omitempty"`
	MaxDiagnosticLogSizeMB      *uint64  `toml:"max_diagnostic_log_size_mb,omitempty"`
	MaxDiagnosticLogBackups     *int     `toml:"max_diagnostic_log_backups,omitempty"`
	MaxDiagnosticLogAgeDays     *int     `toml:"max_diagnostic_log_age_days,omitempty"`
}

func DefaultServerSettings() ServerSettings {
	defaultDiagSizeMB := uint64(20)
	defaultDiagBackups := 5
	defaultDiagAgeDays := 14
	return ServerSettings{
		ListenAddress:               "/ip4/0.0.0.0/tcp/0",
		WebUIListenAddress:          "0.0.0.0:8090",
		EncryptionKeyHex:            "",
		ServerIdentityKeySeedHex:    "",
		DatabasePath:                "activity_server.sqlite",
		LogRetentionDays:            30,
		LogDeletionCheckIntervalHrs: 24,
		BootstrapAddresses:          []string{},
		InternalLogLevel:            "info",
		InternalLogFileDir:          "server_logs",
		InternalLogFileName:         "local_server_diag.log",
		MaxDiagnosticLogSizeMB:      &defaultDiagSizeMB,
		MaxDiagnosticLogBackups:     &defaultDiagBackups,
		MaxDiagnosticLogAgeDays:     &defaultDiagAgeDays,
	}
}

// Methods to get values or defaults for README and templates
func (ss ServerSettings) GetMaxDiagnosticLogSizeMBOrDefault() uint64 {
	if ss.MaxDiagnosticLogSizeMB != nil {
		return *ss.MaxDiagnosticLogSizeMB
	}
	def := DefaultServerSettings()
	if def.MaxDiagnosticLogSizeMB != nil {
		return *def.MaxDiagnosticLogSizeMB
	}
	return 20 // Absolute fallback
}

func (ss ServerSettings) GetMaxDiagnosticLogBackupsOrDefault() int {
	if ss.MaxDiagnosticLogBackups != nil {
		return *ss.MaxDiagnosticLogBackups
	}
	def := DefaultServerSettings()
	if def.MaxDiagnosticLogBackups != nil {
		return *def.MaxDiagnosticLogBackups
	}
	return 5 // Absolute fallback
}

func (ss ServerSettings) GetMaxDiagnosticLogAgeDaysOrDefault() int {
	if ss.MaxDiagnosticLogAgeDays != nil {
		return *ss.MaxDiagnosticLogAgeDays
	}
	def := DefaultServerSettings()
	if def.MaxDiagnosticLogAgeDays != nil {
		return *def.MaxDiagnosticLogAgeDays
	}
	return 14 // Absolute fallback
}
