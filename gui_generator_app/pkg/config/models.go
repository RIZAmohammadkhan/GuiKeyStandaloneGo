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
	MaxLogFileSizeMB                   *uint64  `toml:"max_log_file_size_mb"`
	MaxEventsPerSyncBatch              int      `toml:"max_events_per_sync_batch"`
	InternalLogFileDir                 string   `toml:"internal_log_file_dir"`
	InternalLogFileName                string   `toml:"internal_log_file_name"`
	MaxDiagnosticLogBackups            *int     `toml:"max_diagnostic_log_backups,omitempty"`
	MaxDiagnosticLogAgeDays            *int     `toml:"max_diagnostic_log_age_days,omitempty"`
}

func DefaultClientSettings() ClientSettings {
	defaultSizeMB := uint64(10)
	defaultBackups := 3
	defaultAgeDays := 7

	return ClientSettings{
		ServerPeerID:     "",
		EncryptionKeyHex: "",
		BootstrapAddresses: []string{
			"/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN",
			"/dnsaddr/bootstrap.libp2p.io/p2p/QmQCU2EcMqAqQPR2i9bChDtGNJchTbq5TbXJJ16u19uLTa",
			"/dnsaddr/bootstrap.libp2p.io/p2p/QmbLHAnMoJPWSCR5Zhtx6BHJX9KiKNN6tpvbUcqanj75Nb",
			"/dnsaddr/bootstrap.libp2p.io/p2p/QmcZf59bWwK5XFi76CZX8cbJ4BhTzzA3gU1ZjYZcYW3dwt",
			"/ip4/104.131.131.82/tcp/4001/p2p/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ",         // Test server
			"/ip4/104.131.131.82/udp/4001/quic-v1/p2p/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ", // Test server
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

func (cs ClientSettings) GetMaxLogFileSizeMBOrDefault() uint64 {
	if cs.MaxLogFileSizeMB != nil {
		return *cs.MaxLogFileSizeMB
	}
	def := DefaultClientSettings()
	if def.MaxLogFileSizeMB != nil {
		return *def.MaxLogFileSizeMB
	}
	return 10
}

func (cs ClientSettings) GetMaxDiagnosticLogBackupsOrDefault() int {
	if cs.MaxDiagnosticLogBackups != nil {
		return *cs.MaxDiagnosticLogBackups
	}
	def := DefaultClientSettings()
	if def.MaxDiagnosticLogBackups != nil {
		return *def.MaxDiagnosticLogBackups
	}
	return 3
}

func (cs ClientSettings) GetMaxDiagnosticLogAgeDaysOrDefault() int {
	if cs.MaxDiagnosticLogAgeDays != nil {
		return *cs.MaxDiagnosticLogAgeDays
	}
	def := DefaultClientSettings()
	if def.MaxDiagnosticLogAgeDays != nil {
		return *def.MaxDiagnosticLogAgeDays
	}
	return 7
}

// For local_server_config.toml (used by generator to structure data for server's embedded config)
type ServerSettings struct {
	// ListenAddress is now dynamically determined by the server's P2P manager based on ExplicitP2PPort.
	// It's not directly set in the config file that the generator writes, but the generated code uses ExplicitP2PPort.
	ExplicitP2PPort             *uint16  `toml:"explicit_p2p_port,omitempty"` // User-specified TCP port for P2P
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
		ExplicitP2PPort:             nil, // Default to dynamic port; server will listen on /ip4/0.0.0.0/tcp/0 and /ip4/0.0.0.0/udp/0/quic-v1 etc.
		WebUIListenAddress:          "0.0.0.0:8090",
		EncryptionKeyHex:            "",
		ServerIdentityKeySeedHex:    "",
		DatabasePath:                "activity_server.sqlite",
		LogRetentionDays:            30,
		LogDeletionCheckIntervalHrs: 24,
		BootstrapAddresses:          DefaultClientSettings().BootstrapAddresses, // Use same defaults as client
		InternalLogLevel:            "info",
		InternalLogFileDir:          "server_logs",
		InternalLogFileName:         "local_server_diag.log",
		MaxDiagnosticLogSizeMB:      &defaultDiagSizeMB,
		MaxDiagnosticLogBackups:     &defaultDiagBackups,
		MaxDiagnosticLogAgeDays:     &defaultDiagAgeDays,
	}
}

// Method for template to safely get the port value if set
func (ss ServerSettings) GetExplicitP2PPortValue() uint16 {
	if ss.ExplicitP2PPort != nil {
		return *ss.ExplicitP2PPort
	}
	return 0 // Indicates dynamic
}

func (ss ServerSettings) GetMaxDiagnosticLogSizeMBOrDefault() uint64 {
	if ss.MaxDiagnosticLogSizeMB != nil {
		return *ss.MaxDiagnosticLogSizeMB
	}
	def := DefaultServerSettings()
	if def.MaxDiagnosticLogSizeMB != nil {
		return *def.MaxDiagnosticLogSizeMB
	}
	return 20
}

func (ss ServerSettings) GetMaxDiagnosticLogBackupsOrDefault() int {
	if ss.MaxDiagnosticLogBackups != nil {
		return *ss.MaxDiagnosticLogBackups
	}
	def := DefaultServerSettings()
	if def.MaxDiagnosticLogBackups != nil {
		return *def.MaxDiagnosticLogBackups
	}
	return 5
}

func (ss ServerSettings) GetMaxDiagnosticLogAgeDaysOrDefault() int {
	if ss.MaxDiagnosticLogAgeDays != nil {
		return *ss.MaxDiagnosticLogAgeDays
	}
	def := DefaultServerSettings()
	if def.MaxDiagnosticLogAgeDays != nil {
		return *def.MaxDiagnosticLogAgeDays
	}
	return 14
}
