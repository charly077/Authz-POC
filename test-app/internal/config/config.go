package config

import "time"

var (
	ExternalURL string
	AuditURL    string
	OpenfgaURL  string
	FgaStoreId  string
	FgaModelId  string
	FgaReady    bool
	StartTime   = time.Now()
)
