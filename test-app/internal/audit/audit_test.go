package audit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"test-app/internal/config"
)

func TestSendAuditLog_EmptyURL(t *testing.T) {
	origURL := config.AuditURL
	defer func() { config.AuditURL = origURL }()

	config.AuditURL = ""
	// Should return immediately without error
	SendAuditLog("test", "allow", "alice", "viewer", "animal:1", "CHECK", "reason")
}

func TestSendAuditLog_WithServer(t *testing.T) {
	var mu sync.Mutex
	var received map[string]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path != "/audit" {
			t.Errorf("path = %q, want /audit", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		received = body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	origURL := config.AuditURL
	defer func() { config.AuditURL = origURL }()
	config.AuditURL = server.URL

	SendAuditLog("test-source", "allow", "alice", "viewer", "animal:1", "CHECK", "test reason")

	// SendAuditLog fires an internal goroutine; poll until it completes
	for i := 0; i < 100; i++ {
		mu.Lock()
		got := received
		mu.Unlock()
		if got != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if received == nil {
		t.Fatal("audit log was never received")
	}
	if received["source"] != "test-source" {
		t.Errorf("source = %q, want test-source", received["source"])
	}
	if received["user"] != "alice" {
		t.Errorf("user = %q, want alice", received["user"])
	}
}
