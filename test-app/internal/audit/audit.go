package audit

import (
	"bytes"
	"encoding/json"
	"net/http"

	"test-app/internal/config"
)

func SendAuditLog(source, decision, user, relation, resource, method, reason string) {
	if config.AuditURL == "" {
		return
	}
	go func() {
		entry := map[string]string{
			"source":   source,
			"decision": decision,
			"user":     user,
			"relation": relation,
			"resource": resource,
			"method":   method,
			"reason":   reason,
		}
		b, _ := json.Marshal(entry)
		resp, err := http.Post(config.AuditURL+"/audit", "application/json", bytes.NewReader(b))
		if err != nil {
			return
		}
		resp.Body.Close()
	}()
}
