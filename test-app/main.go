package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var startTime = time.Now()
var externalURL string
var auditURL string

// ──────────────────────────────────────
// OpenFGA config + helpers
// ──────────────────────────────────────

var (
	openfgaURL string
	fgaStoreId string
	fgaModelId string
	fgaReady   bool
)

type fgaConfig struct {
	StoreId string `json:"storeId"`
	ModelId string `json:"modelId"`
}

func loadFGAConfig() {
	configPath := "/shared/openfga-store.json"
	for attempt := 1; attempt <= 30; attempt++ {
		data, err := os.ReadFile(configPath)
		if err == nil {
			var cfg fgaConfig
			if unmarshalErr := json.Unmarshal(data, &cfg); unmarshalErr != nil {
				log.Printf("WARNING: failed to parse FGA config: %v", unmarshalErr)
			} else if cfg.StoreId != "" && cfg.ModelId != "" {
				fgaStoreId = cfg.StoreId
				fgaModelId = cfg.ModelId
				fgaReady = true
				log.Printf("Loaded OpenFGA config: store=%s model=%s", fgaStoreId, fgaModelId)
				rehydrateTuples()
				return
			}
		}
		log.Printf("Waiting for OpenFGA config (%d/30)...", attempt)
		time.Sleep(3 * time.Second)
	}
	log.Println("WARNING: Could not load OpenFGA config after 30 attempts")
}

func fgaRequest(method, path string, body interface{}) (map[string]interface{}, error) {
	var reqBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, openfgaURL+path, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode FGA response: %w", err)
	}
	return result, nil
}

type tupleKey struct {
	User     string `json:"user"`
	Relation string `json:"relation"`
	Object   string `json:"object"`
}

func fgaWrite(writes []tupleKey, deletes []tupleKey) error {
	body := map[string]interface{}{}
	if len(writes) > 0 {
		body["writes"] = map[string]interface{}{"tuple_keys": writes}
	}
	if len(deletes) > 0 {
		body["deletes"] = map[string]interface{}{"tuple_keys": deletes}
	}
	_, err := fgaRequest("POST", "/stores/"+fgaStoreId+"/write", body)
	if err == nil {
		for _, t := range writes {
			sendAuditLog("OpenFGA", "write", t.User, t.Relation, t.Object, "WRITE", "Tuple added: "+t.User+" "+t.Relation+" "+t.Object)
		}
		for _, t := range deletes {
			sendAuditLog("OpenFGA", "delete", t.User, t.Relation, t.Object, "WRITE", "Tuple deleted: "+t.User+" "+t.Relation+" "+t.Object)
		}
	}
	return err
}

func fgaCheck(user, relation, object string) bool {
	body := map[string]interface{}{
		"tuple_key":              map[string]string{"user": user, "relation": relation, "object": object},
		"authorization_model_id": fgaModelId,
	}
	result, err := fgaRequest("POST", "/stores/"+fgaStoreId+"/check", body)
	if err != nil {
		sendAuditLog("OpenFGA", "deny", user, relation, object, "CHECK", "Error: "+err.Error())
		return false
	}
	allowed, _ := result["allowed"].(bool)
	decision := "deny"
	reason := user + " does not have " + relation + " on " + object
	if allowed {
		decision = "allow"
		reason = user + " has " + relation + " on " + object
	}
	sendAuditLog("OpenFGA", decision, user, relation, object, "CHECK", reason)
	return allowed
}

func fgaListObjects(user, relation, typeName string) []string {
	body := map[string]interface{}{
		"user":                   user,
		"relation":               relation,
		"type":                   typeName,
		"authorization_model_id": fgaModelId,
	}
	result, err := fgaRequest("POST", "/stores/"+fgaStoreId+"/list-objects", body)
	if err != nil {
		sendAuditLog("OpenFGA", "deny", user, relation, typeName+":*", "LIST", "Error: "+err.Error())
		return nil
	}
	objects, ok := result["objects"].([]interface{})
	if !ok {
		sendAuditLog("OpenFGA", "allow", user, relation, typeName+":*", "LIST", fmt.Sprintf("Listed 0 %s objects", typeName))
		return nil
	}
	var out []string
	for _, o := range objects {
		if s, ok := o.(string); ok {
			out = append(out, s)
		}
	}
	sendAuditLog("OpenFGA", "allow", user, relation, typeName+":*", "LIST", fmt.Sprintf("Listed %d %s objects", len(out), typeName))
	return out
}

// ──────────────────────────────────────
// Audit log helper — fire-and-forget POST to AI Manager
// ──────────────────────────────────────

func sendAuditLog(source, decision, user, relation, resource, method, reason string) {
	if auditURL == "" {
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
		resp, err := http.Post(auditURL+"/audit", "application/json", bytes.NewReader(b))
		if err != nil {
			return
		}
		resp.Body.Close()
	}()
}

// ──────────────────────────────────────
// Data store with JSON persistence
// ──────────────────────────────────────

type Animal struct {
	Name     string     `json:"name"`
	Species  string     `json:"species"`
	Age      int        `json:"age"`
	Owner    string     `json:"owner"`
	ParentId string     `json:"parentId,omitempty"`
	Relations []Relation `json:"relations,omitempty"`
}

type Relation struct {
	User     string `json:"user"`
	Relation string `json:"relation"`
}

type FriendRequest struct {
	Id     string `json:"id"`
	From   string `json:"from"`
	To     string `json:"to"`
	Status string `json:"status"`
}

type DataStore struct {
	Animals        map[string]*Animal `json:"animals"`
	FriendRequests []FriendRequest    `json:"friendRequests"`
	Friends        map[string][]string `json:"friends"`
}

var (
	dataStore = &DataStore{
		Animals:        make(map[string]*Animal),
		FriendRequests: []FriendRequest{},
		Friends:        make(map[string][]string),
	}
	dataMu   sync.RWMutex
	dataFile = "/data/animals.json"
)

func loadData() {
	data, err := os.ReadFile(dataFile)
	if err != nil {
		return
	}
	dataMu.Lock()
	defer dataMu.Unlock()
	if err := json.Unmarshal(data, dataStore); err != nil {
		log.Printf("WARNING: failed to unmarshal data file: %v", err)
		return
	}
	if dataStore.Animals == nil {
		dataStore.Animals = make(map[string]*Animal)
	}
	if dataStore.Friends == nil {
		dataStore.Friends = make(map[string][]string)
	}
}

func saveData() {
	dataMu.Lock()
	defer dataMu.Unlock()
	dir := filepath.Dir(dataFile)
	os.MkdirAll(dir, 0755)
	data, _ := json.MarshalIndent(dataStore, "", "  ")
	os.WriteFile(dataFile, data, 0644)
}

func rehydrateTuples() {
	var writes []tupleKey
	for id, animal := range dataStore.Animals {
		writes = append(writes, tupleKey{User: "user:" + animal.Owner, Relation: "owner", Object: "animal:" + id})
		if animal.ParentId != "" {
			writes = append(writes, tupleKey{User: "animal:" + animal.ParentId, Relation: "parent", Object: "animal:" + id})
		}
		for _, rel := range animal.Relations {
			writes = append(writes, tupleKey{User: "user:" + rel.User, Relation: rel.Relation, Object: "animal:" + id})
		}
	}
	for userId, friendList := range dataStore.Friends {
		for _, friendId := range friendList {
			writes = append(writes, tupleKey{User: "user:" + friendId, Relation: "friend", Object: "user:" + userId})
		}
	}
	// Write in batches of 10
	for i := 0; i < len(writes); i += 10 {
		end := i + 10
		if end > len(writes) {
			end = len(writes)
		}
		if err := fgaWrite(writes[i:end], nil); err != nil {
			log.Printf("Rehydrate batch error: %v", err)
		}
	}
	if len(writes) > 0 {
		log.Printf("Rehydrated %d tuples from persisted data", len(writes))
	}
}

func randId() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

// ──────────────────────────────────────
// HTTP helpers
// ──────────────────────────────────────

func jsonResponse(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	jsonResponse(w, map[string]string{"error": msg}, status)
}

func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json") ||
		r.URL.Query().Get("format") == "json"
}

func getUser(r *http.Request) string {
	user := r.Header.Get("x-current-user")
	if user == "" {
		user = "anonymous"
	}
	return user
}

func readBody(r *http.Request) (map[string]interface{}, error) {
	var m map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("failed to decode request body: %w", err)
	}
	return m, nil
}

func getString(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func getInt(m map[string]interface{}, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// ──────────────────────────────────────
// Page templates
// ──────────────────────────────────────

type PageData struct {
	Username   string
	Roles      string
	RoleList   []string
	Metadata   string
	Path       string
	Method     string
	Time       string
	IsPublic   bool
	Decision   string
	StatusIcon string
}

const pageTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>AuthZ POC{{if .Username}} - {{.Username}}{{end}}</title>
    <link href="https://fonts.googleapis.com/css2?family=Cormorant+Garamond:ital,wght@0,400;0,500;0,600;0,700;1,400;1,500&family=Nunito+Sans:wght@400;500;600;700;800&family=IBM+Plex+Mono:wght@400;500;600&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg: #faf8f5;
            --surface: #f0ebe4;
            --surface-hover: #e8e2d9;
            --border: #e0d8ce;
            --text: #2c2420;
            --text-muted: #8c7e72;
            --rose: #c4a097;
            --rose-deep: #a8786d;
            --rose-bg: #ecddd8;
            --sage: #6b9080;
            --sage-bg: #dfe9e3;
            --sage-deep: #4a7a64;
            --warm-dark: #3d302a;
            --danger: #c0544f;
            --danger-bg: #f5e0de;
        }

        * { box-sizing: border-box; margin: 0; padding: 0; }

        body {
            font-family: 'Nunito Sans', sans-serif;
            background: var(--bg);
            color: var(--text);
            min-height: 100vh;
        }

        nav {
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 1.1rem 2.5rem;
            background: white;
            border-bottom: 1px solid var(--border);
            position: sticky;
            top: 0;
            z-index: 100;
        }

        .nav-brand {
            display: flex;
            align-items: center;
            gap: 0.6rem;
        }

        .nav-logo {
            width: 34px;
            height: 34px;
            background: var(--warm-dark);
            border-radius: 50%;
            display: flex;
            align-items: center;
            justify-content: center;
            font-size: 0.95rem;
            font-weight: 700;
            color: white;
            font-family: 'Cormorant Garamond', serif;
        }

        .nav-title {
            font-family: 'Cormorant Garamond', serif;
            font-size: 1.25rem;
            font-weight: 700;
            color: var(--text);
        }

        .nav-links {
            display: flex;
            align-items: center;
            gap: 0.15rem;
        }

        .nav-links a {
            color: var(--text-muted);
            text-decoration: none;
            padding: 0.45rem 1rem;
            border-radius: 999px;
            font-size: 0.88rem;
            font-weight: 600;
            transition: all 0.2s;
        }

        .nav-links a:hover {
            color: var(--text);
            background: var(--surface);
        }

        .nav-links a.active {
            color: white;
            background: var(--warm-dark);
        }

        .nav-user {
            display: flex;
            align-items: center;
            gap: 0.75rem;
        }

        .user-badge {
            display: flex;
            align-items: center;
            gap: 0.5rem;
            padding: 0.35rem 0.85rem;
            background: var(--surface);
            border-radius: 999px;
        }

        .user-avatar {
            width: 26px;
            height: 26px;
            border-radius: 50%;
            background: var(--rose);
            display: flex;
            align-items: center;
            justify-content: center;
            font-size: 0.72rem;
            font-weight: 800;
            color: white;
            text-transform: uppercase;
        }

        .user-name {
            font-size: 0.88rem;
            font-weight: 600;
            color: var(--text);
        }

        .btn-logout {
            display: inline-flex;
            align-items: center;
            padding: 0.4rem 1rem;
            background: transparent;
            border: 1.5px solid var(--border);
            color: var(--text-muted);
            border-radius: 999px;
            text-decoration: none;
            font-size: 0.82rem;
            font-weight: 600;
            transition: all 0.2s;
        }

        .btn-logout:hover {
            border-color: var(--danger);
            color: var(--danger);
            background: var(--danger-bg);
        }

        .container {
            max-width: 920px;
            margin: 0 auto;
            padding: 3rem 2rem;
        }

        .page-header {
            margin-bottom: 2.5rem;
            text-align: center;
        }

        .page-header h1 {
            font-family: 'Cormorant Garamond', serif;
            font-size: 3.2rem;
            font-weight: 700;
            line-height: 1.15;
            margin-bottom: 0.75rem;
            color: var(--text);
        }

        .page-header h1 em {
            font-style: italic;
            color: var(--rose-deep);
        }

        .page-header p {
            color: var(--text-muted);
            font-size: 1.02rem;
            max-width: 520px;
            margin: 0 auto;
            line-height: 1.6;
        }

        .decision-box {
            border-radius: 16px;
            padding: 1.25rem 1.5rem;
            margin-bottom: 2rem;
            display: flex;
            align-items: flex-start;
            gap: 1rem;
        }

        .decision-box.allowed {
            background: var(--sage-bg);
        }

        .decision-box.public {
            background: var(--rose-bg);
        }

        .decision-icon {
            width: 42px;
            height: 42px;
            border-radius: 50%;
            display: flex;
            align-items: center;
            justify-content: center;
            font-size: 1.2rem;
            flex-shrink: 0;
        }

        .decision-box.allowed .decision-icon { background: rgba(107, 144, 128, 0.2); }
        .decision-box.public .decision-icon { background: rgba(196, 160, 151, 0.25); }

        .decision-content h3 {
            font-family: 'Cormorant Garamond', serif;
            font-size: 1.15rem;
            font-weight: 700;
            margin-bottom: 0.25rem;
        }

        .decision-box.allowed .decision-content h3 { color: var(--sage-deep); }
        .decision-box.public .decision-content h3 { color: var(--rose-deep); }

        .decision-content p {
            color: var(--text-muted);
            font-size: 0.9rem;
            line-height: 1.65;
        }

        .info-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
            gap: 0.85rem;
            margin-bottom: 2rem;
        }

        .info-card {
            background: white;
            border-radius: 14px;
            padding: 1.15rem 1.25rem;
            box-shadow: 0 1px 3px rgba(0,0,0,0.04);
            transition: box-shadow 0.2s;
        }

        .info-card:hover {
            box-shadow: 0 4px 12px rgba(0,0,0,0.06);
        }

        .info-card-label {
            font-size: 0.72rem;
            font-weight: 700;
            text-transform: uppercase;
            letter-spacing: 0.08em;
            color: var(--text-muted);
            margin-bottom: 0.4rem;
        }

        .info-card-value {
            font-size: 1.05rem;
            font-weight: 600;
            color: var(--text);
        }

        .info-card-value.mono {
            font-family: 'IBM Plex Mono', monospace;
            font-size: 0.82rem;
            color: var(--rose-deep);
        }

        .role-pills {
            display: flex;
            flex-wrap: wrap;
            gap: 0.35rem;
        }

        .role-pill {
            padding: 0.22rem 0.65rem;
            border-radius: 999px;
            font-size: 0.75rem;
            font-weight: 700;
            background: var(--sage-bg);
            color: var(--sage-deep);
        }

        .endpoints-section h2 {
            font-family: 'Cormorant Garamond', serif;
            font-size: 1.6rem;
            font-weight: 700;
            margin-bottom: 1rem;
            color: var(--text);
        }

        .endpoint-list {
            display: flex;
            flex-direction: column;
            gap: 0.5rem;
        }

        .endpoint-item {
            display: flex;
            align-items: center;
            gap: 1rem;
            padding: 1rem 1.25rem;
            background: white;
            border-radius: 14px;
            text-decoration: none;
            color: var(--text);
            box-shadow: 0 1px 3px rgba(0,0,0,0.04);
            transition: all 0.2s;
        }

        .endpoint-item:hover {
            box-shadow: 0 4px 16px rgba(0,0,0,0.08);
            transform: translateY(-1px);
        }

        .endpoint-method {
            padding: 0.18rem 0.55rem;
            border-radius: 6px;
            font-size: 0.68rem;
            font-weight: 700;
            font-family: 'IBM Plex Mono', monospace;
            background: var(--rose-bg);
            color: var(--rose-deep);
            letter-spacing: 0.03em;
        }

        .endpoint-path {
            font-family: 'IBM Plex Mono', monospace;
            font-size: 0.88rem;
            color: var(--text);
            font-weight: 500;
        }

        .endpoint-desc {
            color: var(--text-muted);
            font-size: 0.82rem;
            margin-left: auto;
        }

        .endpoint-arrow {
            color: var(--rose);
            font-size: 1.1rem;
            opacity: 0;
            transition: opacity 0.2s, transform 0.2s;
            transform: translateX(-4px);
        }

        .endpoint-item:hover .endpoint-arrow {
            opacity: 1;
            transform: translateX(0);
        }

        footer {
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 2rem 2.5rem;
            color: var(--text-muted);
            font-size: 0.8rem;
            border-top: 1px solid var(--border);
            margin-top: 3rem;
        }

        footer a { color: var(--rose-deep); text-decoration: none; font-weight: 600; }

        .btn-rule-builder {
            display: inline-flex;
            align-items: center;
            gap: 0.5rem;
            padding: 0.55rem 1.3rem;
            background: var(--warm-dark);
            color: white;
            border-radius: 999px;
            text-decoration: none;
            font-size: 0.85rem;
            font-weight: 700;
            transition: all 0.2s;
        }

        .btn-rule-builder:hover {
            background: #2c2420;
            box-shadow: 0 4px 14px rgba(61,48,42,0.25);
        }

        .section-title {
            font-family: 'Cormorant Garamond', serif;
            font-size: 1.6rem;
            font-weight: 700;
            color: var(--text);
            margin-top: 2.5rem;
            margin-bottom: 0.75rem;
        }

        .prose-card {
            background: white;
            border-radius: 14px;
            padding: 1.75rem;
            margin-bottom: 1rem;
            line-height: 1.75;
            color: #5a4d44;
            font-size: 0.95rem;
            box-shadow: 0 1px 3px rgba(0,0,0,0.04);
        }

        .prose-card p { margin-bottom: 0.75rem; }
        .prose-card p:last-child { margin-bottom: 0; }
        .prose-card code {
            background: var(--rose-bg);
            padding: 0.15rem 0.45rem;
            border-radius: 5px;
            font-family: 'IBM Plex Mono', monospace;
            font-size: 0.84rem;
            color: var(--rose-deep);
        }
        .prose-card a { color: var(--rose-deep); text-decoration: none; font-weight: 700; }
        .prose-card a:hover { text-decoration: underline; }
        .prose-card strong { color: var(--text); }

        .two-col {
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: 0.85rem;
            margin: 1.25rem 0;
        }

        .model-card {
            background: var(--bg);
            border-radius: 14px;
            padding: 1.5rem;
            text-align: center;
        }

        .model-icon {
            width: 56px;
            height: 56px;
            border-radius: 50%;
            background: var(--surface);
            display: flex;
            align-items: center;
            justify-content: center;
            font-size: 1.5rem;
            margin: 0 auto 0.75rem;
        }
        .model-name {
            font-family: 'Cormorant Garamond', serif;
            font-weight: 700;
            font-size: 1.05rem;
            margin-bottom: 0.4rem;
            color: var(--text);
        }
        .model-desc { font-size: 0.88rem; color: var(--text-muted); line-height: 1.65; text-align: left; }
        .model-desc strong { color: #5a4d44; }

        .arch-flow {
            display: flex;
            align-items: center;
            gap: 0.5rem;
            flex-wrap: wrap;
            justify-content: center;
        }

        .arch-step {
            display: flex;
            align-items: center;
            gap: 0.5rem;
            background: var(--bg);
            border-radius: 999px;
            padding: 0.55rem 1.1rem;
            font-size: 0.88rem;
        }

        .arch-num {
            display: inline-flex;
            align-items: center;
            justify-content: center;
            width: 24px;
            height: 24px;
            border-radius: 50%;
            background: var(--warm-dark);
            color: white;
            font-size: 0.72rem;
            font-weight: 800;
            flex-shrink: 0;
        }

        .arch-arrow { color: var(--rose); font-size: 1.2rem; }

        .steps-list { padding-left: 1.5rem; margin: 0; }
        .steps-list li { margin-bottom: 0.65rem; line-height: 1.65; color: #5a4d44; }
        .steps-list li:last-child { margin-bottom: 0; }

        /* Decorative accent */
        .page-header::after {
            content: '';
            display: block;
            width: 48px;
            height: 3px;
            background: var(--rose);
            border-radius: 2px;
            margin: 1.25rem auto 0;
        }

        @media (max-width: 640px) {
            nav { padding: 0.8rem 1rem; flex-wrap: wrap; gap: 0.75rem; }
            .nav-links { display: none; }
            .container { padding: 1.5rem 1.25rem; }
            .page-header h1 { font-size: 2.2rem; }
            .info-grid { grid-template-columns: 1fr; }
            .endpoint-desc { display: none; }
            .two-col { grid-template-columns: 1fr; }
            .arch-flow { flex-direction: column; }
            .arch-arrow { transform: rotate(90deg); }
        }
    </style>
</head>
<body>
    <nav>
        <div class="nav-brand">
            <div class="nav-logo">A</div>
            <span class="nav-title">AuthZ POC</span>
        </div>
        <div class="nav-links">
            <a href="/home"{{if eq .Path "/home"}} class="active"{{end}}>Home</a>
            <a href="/public"{{if eq .Path "/public"}} class="active"{{end}}>Public</a>
            <a href="/api/protected"{{if eq .Path "/api/protected"}} class="active"{{end}}>Protected</a>
            <a href="/animals"{{if eq .Path "/animals"}} class="active"{{end}}>Animals</a>
            <a href="/api/health"{{if eq .Path "/api/health"}} class="active"{{end}}>Health</a>
        </div>
        <div class="nav-user">
            {{if .Username}}
                <div class="user-badge">
                    <div class="user-avatar">{{index .Username 0 | printf "%c"}}</div>
                    <span class="user-name">{{.Username}}</span>
                </div>
                <a href="/logout" class="btn-logout">Sign out</a>
            {{else if not .IsPublic}}
                <a href="/home" class="btn-logout" style="border-color: var(--rose); color: var(--rose-deep); background: var(--rose-bg);">Sign in</a>
            {{end}}
        </div>
    </nav>

    <div class="container">
        {{if eq .Path "/home"}}
            <div class="page-header">
                <h1>Fine-Grained <em>Authorization</em></h1>
                <p>Externalized policy enforcement with OPA, Keycloak, and OpenFGA</p>
            </div>

            {{if .Username}}
            <div class="decision-box allowed">
                <div class="decision-icon">{{.StatusIcon}}</div>
                <div class="decision-content">
                    <h3>Access Granted &mdash; Authenticated</h3>
                    <p>Your identity was verified by <strong>Keycloak</strong> via OIDC. The request was evaluated by <strong>OPA</strong> which extracted your token claims and forwarded them to the application. Authorization decision: <strong>{{.Decision}}</strong>.</p>
                </div>
            </div>
            {{end}}

            <div class="info-grid">
                {{if .Username}}
                <div class="info-card">
                    <div class="info-card-label">Authenticated User</div>
                    <div class="info-card-value">{{.Username}}</div>
                </div>
                <div class="info-card">
                    <div class="info-card-label">Roles</div>
                    <div class="info-card-value">
                        <div class="role-pills">
                            {{range .RoleList}}<span class="role-pill">{{.}}</span>{{end}}
                            {{if not .RoleList}}<span style="color: var(--text-muted); font-size: 0.88rem;">No roles assigned</span>{{end}}
                        </div>
                    </div>
                </div>
                <div class="info-card">
                    <div class="info-card-label">OPA Metadata</div>
                    <div class="info-card-value mono">{{.Metadata}}</div>
                </div>
                <div class="info-card">
                    <div class="info-card-label">Server Time</div>
                    <div class="info-card-value mono">{{.Time}}</div>
                </div>
                {{end}}
            </div>

            <div class="endpoints-section">
                <h2>Available Endpoints</h2>
                <div class="endpoint-list">
                    <a href="/public" class="endpoint-item">
                        <span class="endpoint-method">GET</span>
                        <span class="endpoint-path">/public</span>
                        <span class="endpoint-desc">No authentication required</span>
                        <span class="endpoint-arrow">&rarr;</span>
                    </a>
                    <a href="/api/protected" class="endpoint-item">
                        <span class="endpoint-method">GET</span>
                        <span class="endpoint-path">/api/protected</span>
                        <span class="endpoint-desc">Requires valid Bearer token</span>
                        <span class="endpoint-arrow">&rarr;</span>
                    </a>
                    <a href="/animals" class="endpoint-item">
                        <span class="endpoint-method">GET</span>
                        <span class="endpoint-path">/animals</span>
                        <span class="endpoint-desc">Animals demo with OpenFGA ReBAC</span>
                        <span class="endpoint-arrow">&rarr;</span>
                    </a>
                    <a href="/api/health" class="endpoint-item">
                        <span class="endpoint-method">GET</span>
                        <span class="endpoint-path">/api/health</span>
                        <span class="endpoint-desc">Service health check</span>
                        <span class="endpoint-arrow">&rarr;</span>
                    </a>
                </div>
            </div>

        {{else if .IsPublic}}
            <div class="page-header">
                <h1>Fine-Grained <em>Authorization</em> POC</h1>
                <p>A proof-of-concept combining <strong>OPA</strong> (policy-based) and <strong>OpenFGA</strong> (relationship-based) authorization, externalized from the application.</p>
            </div>

            <div class="decision-box public">
                <div class="decision-icon">{{.StatusIcon}}</div>
                <div class="decision-content">
                    <h3>You are viewing a public page</h3>
                    <p>This page is accessible without authentication. OPA evaluated the path <code>/public</code> and granted access via its <strong>pass-through rule</strong>. No token was required.</p>
                </div>
            </div>

            <div class="section-title">Why this project?</div>
            <div class="prose-card">
                <p>Most applications embed authorization logic directly in code &mdash; <code>if user.role == "admin"</code> scattered everywhere. This is fragile, hard to audit, and impossible to manage at scale.</p>
                <p>This POC explores a better approach: <strong>externalized authorization</strong>. The application never makes access decisions itself. Instead, a dedicated infrastructure layer handles it, combining two complementary models:</p>
                <div class="two-col">
                    <div class="model-card">
                        <div class="model-icon">&#x1F4DC;</div>
                        <div class="model-name">OPA &mdash; Policy-Based (ABAC)</div>
                        <div class="model-desc">Open Policy Agent evaluates <strong>attributes</strong>: request path, HTTP method, JWT claims, time of day, IP ranges. Policies are written in Rego and enforced at the gateway level by Envoy.</div>
                    </div>
                    <div class="model-card">
                        <div class="model-icon">&#x1F517;</div>
                        <div class="model-name">OpenFGA &mdash; Relationship-Based (ReBAC)</div>
                        <div class="model-desc">OpenFGA stores <strong>relationships</strong> between users and resources: &ldquo;Alice is owner of Cat&rdquo;, &ldquo;Bob is friend of Alice&rdquo;. Access is derived from the relationship graph, enabling fine-grained sharing.</div>
                    </div>
                </div>
                <p>Together, OPA handles the <em>&ldquo;can this user access this endpoint?&rdquo;</em> question, while OpenFGA answers <em>&ldquo;can this user see or edit this specific resource?&rdquo;</em></p>
            </div>

            <div class="section-title">Architecture</div>
            <div class="prose-card">
                <div class="arch-flow">
                    <div class="arch-step"><span class="arch-num">1</span><strong>Envoy Proxy</strong> intercepts every request</div>
                    <div class="arch-arrow">&rarr;</div>
                    <div class="arch-step"><span class="arch-num">2</span><strong>Keycloak</strong> authenticates the user (OIDC)</div>
                    <div class="arch-arrow">&rarr;</div>
                    <div class="arch-step"><span class="arch-num">3</span><strong>OPA</strong> evaluates the policy (allow/deny)</div>
                    <div class="arch-arrow">&rarr;</div>
                    <div class="arch-step"><span class="arch-num">4</span><strong>App</strong> queries <strong>OpenFGA</strong> for resource-level access</div>
                </div>
                <p style="margin-top: 1rem;">The application code is completely free of authorization logic. It receives pre-validated identity headers from OPA and delegates relationship checks to OpenFGA.</p>
            </div>

            <div class="section-title">Try it out</div>
            <div class="endpoint-list">
                <a href="/home" class="endpoint-item">
                    <span class="endpoint-method">GET</span>
                    <span class="endpoint-path">/home</span>
                    <span class="endpoint-desc">Authenticated dashboard &mdash; sign in with Keycloak</span>
                    <span class="endpoint-arrow">&rarr;</span>
                </a>
                <a href="/animals" class="endpoint-item">
                    <span class="endpoint-method">GET</span>
                    <span class="endpoint-path">/animals</span>
                    <span class="endpoint-desc">Animals demo &mdash; OpenFGA relationships in action</span>
                    <span class="endpoint-arrow">&rarr;</span>
                </a>
                <a href="/api/protected" class="endpoint-item">
                    <span class="endpoint-method">GET</span>
                    <span class="endpoint-path">/api/protected</span>
                    <span class="endpoint-desc">Protected API &mdash; requires a valid Bearer token</span>
                    <span class="endpoint-arrow">&rarr;</span>
                </a>
                <a href="/manager" class="endpoint-item" target="_blank">
                    <span class="endpoint-method">UI</span>
                    <span class="endpoint-path">/manager</span>
                    <span class="endpoint-desc">AI-powered rule builder (Gemini)</span>
                    <span class="endpoint-arrow">&rarr;</span>
                </a>
            </div>

            <div class="section-title" style="margin-top: 2.5rem;">Demo credentials</div>
            <div class="info-grid">
                <div class="info-card">
                    <div class="info-card-label">User 1</div>
                    <div class="info-card-value">alice / alice</div>
                </div>
                <div class="info-card">
                    <div class="info-card-label">User 2</div>
                    <div class="info-card-value">bob / bob</div>
                </div>
                <div class="info-card">
                    <div class="info-card-label">Keycloak Admin</div>
                    <div class="info-card-value">admin / admin</div>
                </div>
            </div>

            <div class="section-title" style="margin-top: 2.5rem;">How to test</div>
            <div class="prose-card">
                <ol class="steps-list">
                    <li><strong>Sign in</strong> &mdash; Go to <a href="/home">/home</a> and log in as <code>alice</code>. You will be redirected to Keycloak, then back to the app with your identity verified.</li>
                    <li><strong>Create animals</strong> &mdash; On the <a href="/animals">/animals</a> page, create a few animals. You are automatically the <em>owner</em> (stored as an OpenFGA relationship).</li>
                    <li><strong>Add a friend</strong> &mdash; Send a friend request to <code>bob</code>. Then sign in as <code>bob</code> in another browser to accept it.</li>
                    <li><strong>Share access</strong> &mdash; Back as <code>alice</code>, assign <code>bob</code> as <em>editor</em> or <em>viewer</em> on one of your animals. This writes a relationship tuple to OpenFGA.</li>
                    <li><strong>Verify as Bob</strong> &mdash; Sign in as <code>bob</code> and check the <em>Shared With Me</em> section. Bob can now see (and possibly edit) Alice's animal &mdash; determined entirely by the OpenFGA relationship graph.</li>
                    <li><strong>Explore policies</strong> &mdash; Use the <a href="/manager" target="_blank">AI Manager</a> to inspect, modify, or generate OPA policies using natural language.</li>
                </ol>
            </div>

        {{else}}
            <div class="page-header">
                <h1>{{if eq .Path "/api/protected"}}<em>Protected</em> Resource{{else if eq .Path "/api/health"}}Service <em>Health</em>{{else}}{{.Path}}{{end}}</h1>
                <p>{{if eq .Path "/api/protected"}}This endpoint requires authentication. Your identity was verified by Keycloak.{{else if eq .Path "/api/health"}}Real-time service status information.{{else}}Authenticated resource.{{end}}</p>
            </div>

            <div class="decision-box allowed">
                <div class="decision-icon">{{.StatusIcon}}</div>
                <div class="decision-content">
                    <h3>Access Granted &mdash; Policy Passed</h3>
                    <p>OPA validated your Bearer token and evaluated the policy for path <code>{{.Path}}</code>. Decision: <strong>{{.Decision}}</strong>. User identity extracted from JWT claims by OPA and forwarded via headers.</p>
                </div>
            </div>

            <div class="info-grid">
                <div class="info-card">
                    <div class="info-card-label">Authenticated User</div>
                    <div class="info-card-value">{{.Username}}</div>
                </div>
                <div class="info-card">
                    <div class="info-card-label">Roles</div>
                    <div class="info-card-value">
                        <div class="role-pills">
                            {{range .RoleList}}<span class="role-pill">{{.}}</span>{{end}}
                            {{if not .RoleList}}<span style="color: var(--text-muted); font-size: 0.88rem;">No roles assigned</span>{{end}}
                        </div>
                    </div>
                </div>
                <div class="info-card">
                    <div class="info-card-label">OPA Metadata</div>
                    <div class="info-card-value mono">{{.Metadata}}</div>
                </div>
                <div class="info-card">
                    <div class="info-card-label">Request Method</div>
                    <div class="info-card-value mono">{{.Method}}</div>
                </div>
                <div class="info-card">
                    <div class="info-card-label">Request Path</div>
                    <div class="info-card-value mono">{{.Path}}</div>
                </div>
                <div class="info-card">
                    <div class="info-card-label">Server Time</div>
                    <div class="info-card-value mono">{{.Time}}</div>
                </div>
            </div>
        {{end}}
    </div>

    <footer>
        <span>Fine-Grained Authorization POC &middot; Powered by
        <a href="https://www.envoyproxy.io/">Envoy</a>,
        <a href="https://www.openpolicyagent.org/">OPA</a>,
        <a href="https://www.keycloak.org/">Keycloak</a> &amp;
        <a href="https://openfga.dev/">OpenFGA</a></span>
        <a href="/manager" class="btn-rule-builder" target="_blank">AuthZ Rule Builder &#8594;</a>
    </footer>
</body>
</html>`

var tmpl = template.Must(template.New("page").Parse(pageTemplate))

func buildPageData(r *http.Request, isPublic bool) PageData {
	user := r.Header.Get("x-current-user")
	roles := r.Header.Get("x-user-role")
	metadata := r.Header.Get("x-user-metadata")

	var roleList []string
	if roles != "" {
		for _, role := range strings.Split(roles, ",") {
			role = strings.TrimSpace(role)
			if role != "" {
				roleList = append(roleList, role)
			}
		}
	}

	decision := metadata
	if decision == "" {
		decision = "N/A"
	}

	statusIcon := "\u2705" // checkmark
	if isPublic {
		statusIcon = "\U0001F310" // globe
	}

	return PageData{
		Username:   user,
		Roles:      roles,
		RoleList:   roleList,
		Metadata:   metadata,
		Path:       r.URL.Path,
		Method:     r.Method,
		Time:       time.Now().Format(time.RFC3339),
		IsPublic:   isPublic,
		Decision:   decision,
		StatusIcon: statusIcon,
	}
}

// ──────────────────────────────────────
// Animals page template
// ──────────────────────────────────────

const animalsPageTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>AuthZ POC - Animals</title>
    <link href="https://fonts.googleapis.com/css2?family=Cormorant+Garamond:ital,wght@0,400;0,500;0,600;0,700;1,400;1,500&family=Nunito+Sans:wght@400;500;600;700;800&family=IBM+Plex+Mono:wght@400;500;600&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg: #faf8f5; --surface: #f0ebe4; --surface-hover: #e8e2d9;
            --border: #e0d8ce; --text: #2c2420; --text-muted: #8c7e72;
            --rose: #c4a097; --rose-deep: #a8786d; --rose-bg: #ecddd8;
            --sage: #6b9080; --sage-bg: #dfe9e3; --sage-deep: #4a7a64;
            --warm-dark: #3d302a; --danger: #c0544f; --danger-bg: #f5e0de;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: 'Nunito Sans', sans-serif; background: var(--bg); color: var(--text); min-height: 100vh; }

        nav { display: flex; align-items: center; justify-content: space-between; padding: 1.1rem 2.5rem;
            background: white; border-bottom: 1px solid var(--border);
            position: sticky; top: 0; z-index: 100; }
        .nav-brand { display: flex; align-items: center; gap: 0.6rem; }
        .nav-logo { width: 34px; height: 34px; background: var(--warm-dark);
            border-radius: 50%; display: flex; align-items: center; justify-content: center;
            font-size: 0.95rem; font-weight: 700; color: white; font-family: 'Cormorant Garamond', serif; }
        .nav-title { font-family: 'Cormorant Garamond', serif; font-size: 1.25rem; font-weight: 700; color: var(--text); }
        .nav-links { display: flex; align-items: center; gap: 0.15rem; }
        .nav-links a { color: var(--text-muted); text-decoration: none; padding: 0.45rem 1rem; border-radius: 999px;
            font-size: 0.88rem; font-weight: 600; transition: all 0.2s; }
        .nav-links a:hover { color: var(--text); background: var(--surface); }
        .nav-links a.active { color: white; background: var(--warm-dark); }
        .nav-user { display: flex; align-items: center; gap: 0.75rem; }
        .user-badge { display: flex; align-items: center; gap: 0.5rem; padding: 0.35rem 0.85rem;
            background: var(--surface); border-radius: 999px; }
        .user-avatar { width: 26px; height: 26px; border-radius: 50%; background: var(--rose);
            display: flex; align-items: center; justify-content: center;
            font-size: 0.72rem; font-weight: 800; color: white; text-transform: uppercase; }
        .user-name { font-size: 0.88rem; font-weight: 600; color: var(--text); }
        .btn-logout { display: inline-flex; align-items: center; padding: 0.4rem 1rem;
            background: transparent; border: 1.5px solid var(--border); color: var(--text-muted);
            border-radius: 999px; text-decoration: none; font-size: 0.82rem; font-weight: 600; transition: all 0.2s; }
        .btn-logout:hover { border-color: var(--danger); color: var(--danger); background: var(--danger-bg); }

        .container { max-width: 920px; margin: 0 auto; padding: 3rem 2rem; }
        .page-header { margin-bottom: 2.5rem; }
        .page-header h1 { font-family: 'Cormorant Garamond', serif; font-size: 2.6rem; font-weight: 700;
            line-height: 1.15; margin-bottom: 0.5rem; }
        .page-header h1 em { font-style: italic; color: var(--rose-deep); }
        .page-header p { color: var(--text-muted); font-size: 0.95rem; }

        .card { background: white; border-radius: 14px; padding: 1.5rem; margin-bottom: 1.5rem;
            box-shadow: 0 1px 3px rgba(0,0,0,0.04); }
        .card h3 { font-family: 'Cormorant Garamond', serif; color: var(--text); margin-bottom: 0.75rem;
            font-size: 1.2rem; font-weight: 700; }
        .card h4 { color: var(--text-muted); font-size: 0.78rem; margin-top: 0.75rem; margin-bottom: 0.25rem;
            font-weight: 700; text-transform: uppercase; letter-spacing: 0.06em; }

        .top-row { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; margin-bottom: 1.5rem; }
        .friends-list { margin-bottom: 0.75rem; }
        .friend-item { display: flex; align-items: center; gap: 0.5rem; padding: 0.4rem 0; font-weight: 500; }
        .friend-request-form { display: flex; gap: 0.5rem; margin-top: 0.75rem; }

        input[type="text"], input[type="number"], select {
            width: 100%; padding: 0.55rem 0.85rem; margin-bottom: 0.5rem; border-radius: 10px;
            border: 1.5px solid var(--border); background: var(--bg); color: var(--text);
            font-family: 'Nunito Sans', sans-serif; font-size: 0.9rem; }
        input:focus, select:focus { outline: none; border-color: var(--rose); }

        .friend-request-form select { flex: 1; margin-bottom: 0; }
        .friend-request-form input { flex: 1; margin-bottom: 0; }

        .btn { padding: 0.5rem 1rem; border-radius: 999px; font-weight: 700; cursor: pointer; border: none;
            transition: all 0.2s; font-family: 'Nunito Sans', sans-serif; font-size: 0.85rem; }
        .btn-primary { background: var(--warm-dark); color: white; }
        .btn-primary:hover { background: #2c2420; box-shadow: 0 4px 14px rgba(61,48,42,0.2); }
        .btn-danger { background: var(--danger-bg); color: var(--danger); }
        .btn-danger:hover { background: #f0ccc9; }
        .btn-success { background: var(--sage-bg); color: var(--sage-deep); }
        .btn-success:hover { background: #d0dfd5; }
        .btn-secondary { background: var(--surface); color: var(--text); }
        .btn-sm { padding: 0.35rem 0.8rem; font-size: 0.78rem; }
        .btn-xs { padding: 0.22rem 0.55rem; font-size: 0.72rem; line-height: 1; }

        .animals-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 0.85rem; margin-top: 0.75rem; }
        .animal-card { background: var(--bg); border-radius: 14px; padding: 1.1rem;
            transition: box-shadow 0.2s; box-shadow: 0 1px 2px rgba(0,0,0,0.03); }
        .animal-card:hover { box-shadow: 0 4px 14px rgba(0,0,0,0.07); }
        .animal-card-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem; }
        .animal-card-header strong { font-family: 'Cormorant Garamond', serif; font-weight: 700; font-size: 1.1rem; }
        .animal-owner { color: var(--text-muted); font-size: 0.78rem; }
        .species-badge { display: inline-block; padding: 0.15rem 0.6rem; border-radius: 999px; font-size: 0.72rem;
            font-weight: 700; background: var(--rose-bg); color: var(--rose-deep); margin-right: 0.5rem; }
        .animal-age { color: var(--text-muted); font-size: 0.82rem; }
        .animal-actions { margin-top: 0.75rem; display: flex; gap: 0.4rem; }
        .animal-relations { margin-top: 0.75rem; padding-top: 0.75rem; border-top: 1px solid var(--border); }
        .animal-relations h5 { color: var(--text-muted);
            font-size: 0.72rem; margin-bottom: 0.4rem; text-transform: uppercase; letter-spacing: 0.06em; font-weight: 700; }
        .relation-item { display: flex; align-items: center; gap: 0.4rem; padding: 0.2rem 0; font-size: 0.82rem; }
        .relation-badge { display: inline-block; padding: 0.12rem 0.5rem; border-radius: 999px; font-size: 0.65rem;
            font-weight: 700; text-transform: uppercase; }
        .relation-owner { background: #faf0d4; color: #9a7b2c; }
        .relation-editor { background: #dce8f8; color: #3b6fb5; }
        .relation-know { background: var(--sage-bg); color: var(--sage-deep); }
        .add-relation-form { display: flex; gap: 0.35rem; margin-top: 0.4rem; }
        .add-relation-form select { flex: 1; margin-bottom: 0; padding: 0.25rem 0.4rem; font-size: 0.72rem; }
        .add-relation-form select:last-of-type { width: 65px; flex: none; }

        .muted { color: var(--text-muted); font-size: 0.82rem; }

        .debug-section { margin-top: 1rem; }
        .debug-toggle { cursor: pointer; user-select: none; color: var(--text-muted); font-size: 0.9rem; }
        .debug-table { width: 100%; border-collapse: collapse; margin-top: 0.75rem; font-size: 0.82rem; }
        .debug-table th, .debug-table td { text-align: left; padding: 0.5rem 0.75rem; border-bottom: 1px solid var(--border); }
        .debug-table th { color: var(--rose-deep); font-weight: 700; text-transform: uppercase;
            letter-spacing: 0.04em; font-size: 0.72rem; }
        .debug-table td { color: #5a4d44; font-family: 'IBM Plex Mono', monospace; font-size: 0.78rem; }

        .toast { position: fixed; bottom: 2rem; right: 2rem; padding: 0.9rem 1.4rem; border-radius: 999px;
            color: white; font-weight: 700; z-index: 1000; font-size: 0.88rem;
            animation: slideIn 0.3s ease, fadeOut 0.3s ease 2.7s forwards; }
        .toast-success { background: var(--sage-deep); }
        .toast-error { background: var(--danger); }
        @keyframes slideIn { from { transform: translateX(100%); opacity: 0; } to { transform: translateX(0); opacity: 1; } }
        @keyframes fadeOut { from { opacity: 1; } to { opacity: 0; } }

        footer { display: flex; align-items: center; justify-content: space-between; padding: 2rem 2.5rem;
            color: var(--text-muted); font-size: 0.8rem; border-top: 1px solid var(--border); margin-top: 3rem; }
        footer a { color: var(--rose-deep); text-decoration: none; font-weight: 700; }

        .ai-explain-box { background: white; border-radius: 14px; padding: 1.5rem; margin-bottom: 1.5rem;
            box-shadow: 0 1px 3px rgba(0,0,0,0.04); }
        .ai-explain-box h3 { font-family: 'Cormorant Garamond', serif; color: var(--text);
            margin-bottom: 0.5rem; font-size: 1.2rem; font-weight: 700; }
        .ai-explain-box p.desc { color: var(--text-muted); font-size: 0.88rem; margin-bottom: 1rem; }
        .ai-explain-btn { padding: 0.6rem 1.4rem; border-radius: 999px; font-weight: 700; cursor: pointer; border: none;
            background: var(--warm-dark); color: white; font-family: 'Nunito Sans', sans-serif;
            font-size: 0.9rem; transition: all 0.2s; }
        .ai-explain-btn:hover { background: #2c2420; box-shadow: 0 4px 14px rgba(61,48,42,0.25); }
        .ai-explain-btn:disabled { opacity: 0.5; cursor: not-allowed; box-shadow: none; }
        .ai-explain-spinner { display: inline-block; width: 14px; height: 14px; border: 2px solid rgba(255,255,255,0.3);
            border-top-color: white; border-radius: 50%; animation: aispin 0.6s linear infinite; margin-right: 0.4rem; vertical-align: middle; }
        @keyframes aispin { to { transform: rotate(360deg); } }
        .ai-explain-result { margin-top: 1rem; padding: 1.25rem; background: var(--bg);
            border-radius: 12px; line-height: 1.75; font-size: 0.9rem; color: #5a4d44; }
        .ai-explain-result h1, .ai-explain-result h2, .ai-explain-result h3, .ai-explain-result h4 {
            font-family: 'Cormorant Garamond', serif; color: var(--text); margin: 1rem 0 0.5rem 0; }
        .ai-explain-result h1 { font-size: 1.3rem; } .ai-explain-result h2 { font-size: 1.15rem; }
        .ai-explain-result h3 { font-size: 1.05rem; } .ai-explain-result h4 { font-size: 0.95rem; }
        .ai-explain-result ul, .ai-explain-result ol { padding-left: 1.5rem; margin: 0.5rem 0; }
        .ai-explain-result li { margin-bottom: 0.3rem; }
        .ai-explain-result code { background: var(--rose-bg); padding: 0.12rem 0.4rem; border-radius: 5px;
            font-family: 'IBM Plex Mono', monospace; font-size: 0.82rem; color: var(--rose-deep); }
        .ai-explain-result strong { color: var(--text); }
        .ai-explain-error { color: var(--danger); margin-top: 1rem; }

        @media (max-width: 640px) {
            nav { padding: 0.8rem 1rem; flex-wrap: wrap; gap: 0.75rem; }
            .nav-links { display: none; }
            .container { padding: 1.5rem 1.25rem; }
            .page-header h1 { font-size: 1.9rem; }
            .top-row { grid-template-columns: 1fr; }
        }
    </style>
</head>
<body>
    <nav>
        <div class="nav-brand">
            <div class="nav-logo">A</div>
            <span class="nav-title">AuthZ POC</span>
        </div>
        <div class="nav-links">
            <a href="/home">Home</a>
            <a href="/public">Public</a>
            <a href="/api/protected">Protected</a>
            <a href="/animals" class="active">Animals</a>
            <a href="/api/health">Health</a>
        </div>
        <div class="nav-user">
            <div class="user-badge">
                <div class="user-avatar">{{index .Username 0 | printf "%c"}}</div>
                <span class="user-name">{{.Username}}</span>
            </div>
            <a href="/logout" class="btn-logout">Sign out</a>
        </div>
    </nav>

    <div class="container">
        <div class="page-header">
            <h1><em>Animals</em></h1>
            <p>Manage your animals with OpenFGA relationship-based access control. Logged in as <strong>{{.Username}}</strong>.</p>
        </div>

        <div id="app">Loading...</div>
    </div>

    <footer>
        <span>Fine-Grained Authorization POC</span>
        <a href="/manager" target="_blank">AuthZ Rule Builder &rarr;</a>
    </footer>

    <script>
    const currentUser = '{{.Username}}';
    const apiBase = '/api/animals';

    function escapeHtml(str) {
        const div = document.createElement('div');
        div.textContent = str;
        return div.innerHTML;
    }

    function showToast(msg, type) {
        const t = document.createElement('div');
        t.className = 'toast toast-' + (type || 'success');
        t.textContent = msg;
        document.body.appendChild(t);
        setTimeout(() => t.remove(), 3000);
    }

    async function api(path, opts) {
        const res = await fetch(apiBase + path, {
            headers: { 'Content-Type': 'application/json', ...(opts?.headers || {}) },
            ...opts
        });
        const data = await res.json();
        if (!res.ok) throw new Error(data.error || 'Request failed');
        return data;
    }

    async function render() {
        const app = document.getElementById('app');
        try {
            const [animalsData, friendsData] = await Promise.all([
                api('/list'),
                api('/friends')
            ]);

            const allAnimals = animalsData.animals || [];
            const myAnimals = allAnimals.filter(a => a.owner === currentUser);
            const sharedAnimals = allAnimals.filter(a => a.owner !== currentUser);
            const friends = friendsData.friends || [];
            const incoming = friendsData.incoming || [];
            const outgoing = friendsData.outgoing || [];

            // Known users: from friends, requests, and the two demo users
            const knownUsers = [...new Set(['alice', 'bob', ...friends])].filter(u => u !== currentUser);

            app.innerHTML = '' +
                '<div class="top-row">' +
                '  <div class="card">' +
                '    <h3>Friends</h3>' +
                '    <div class="friends-list">' +
                (friends.length === 0 ? '<p class="muted">No friends yet</p>' :
                    friends.map(f => '<div class="friend-item"><span>' + escapeHtml(f) + '</span>' +
                        '<button class="btn btn-danger btn-sm" onclick="removeFriend(\'' + escapeHtml(f) + '\')">Remove</button></div>').join('')) +
                '    </div>' +
                (incoming.length > 0 ? '<h4>Incoming Requests</h4>' +
                    incoming.map(r => '<div class="friend-item"><span>' + escapeHtml(r.from) + '</span>' +
                        '<button class="btn btn-success btn-sm" onclick="acceptFriend(\'' + r.id + '\')">Accept</button>' +
                        '<button class="btn btn-danger btn-sm" onclick="denyFriend(\'' + r.id + '\')">Deny</button></div>').join('') : '') +
                (outgoing.length > 0 ? '<h4>Outgoing Requests</h4>' +
                    outgoing.map(r => '<div class="friend-item"><span>To: ' + escapeHtml(r.to) + '</span> <span class="muted">pending</span></div>').join('') : '') +
                '    <div class="friend-request-form">' +
                '      <input type="text" id="friendTarget" placeholder="Username">' +
                '      <button class="btn btn-primary btn-sm" onclick="sendFriendRequest()">Send Request</button>' +
                '    </div>' +
                '  </div>' +
                '  <div class="card">' +
                '    <h3>New Animal</h3>' +
                '    <input type="text" id="animalName" placeholder="Name">' +
                '    <input type="text" id="animalSpecies" placeholder="Species">' +
                '    <input type="number" id="animalAge" placeholder="Age" min="0">' +
                '    <button class="btn btn-primary" onclick="createAnimal()">Create</button>' +
                '  </div>' +
                '</div>' +
                '<div class="card">' +
                '  <h3>My Animals</h3>' +
                '  <div class="animals-grid">' +
                (myAnimals.length === 0 ? '<p class="muted">No animals yet. Create one above.</p>' :
                    myAnimals.map(a => renderAnimalCard(a, friends)).join('')) +
                '  </div>' +
                '</div>' +
                (sharedAnimals.length > 0 ? '<div class="card"><h3>Shared With Me</h3><div class="animals-grid">' +
                    sharedAnimals.map(a => renderAnimalCard(a, friends)).join('') + '</div></div>' : '') +
                '<div class="card debug-section">' +
                '  <h3 class="debug-toggle" onclick="toggleDebug()">Debug: OpenFGA Tuples <span id="debugArrow">&#9654;</span></h3>' +
                '  <div id="debugContent" style="display:none;">' +
                '    <button class="btn btn-secondary btn-sm" onclick="refreshDebug()">Refresh</button>' +
                '    <table class="debug-table"><thead><tr><th>User</th><th>Relation</th><th>Object</th></tr></thead>' +
                '    <tbody id="debugBody"></tbody></table>' +
                '  </div>' +
                '</div>' +
                '<div class="ai-explain-box">' +
                '  <h3>AuthZ Decision Explained by AI</h3>' +
                '  <p class="desc">Click the button below to get an AI-powered explanation of your current authorization state — why you can see certain animals and what relationships grant you access.</p>' +
                '  <button class="ai-explain-btn" id="aiExplainBtn" onclick="requestAIExplanation()">Explain My Authorization</button>' +
                '  <div id="aiExplainResult"></div>' +
                '</div>';
        } catch (e) {
            app.innerHTML = '<div class="card"><p>Error loading data: ' + escapeHtml(e.message) + '</p></div>';
        }
    }

    function renderAnimalCard(animal, friends) {
        const rels = animal.relations || [];
        const editable = animal.canEdit;
        let html = '<div class="animal-card">' +
            '<div class="animal-card-header"><strong>' + escapeHtml(animal.name) + '</strong>' +
            (animal.owner !== currentUser ? '<span class="animal-owner">(' + escapeHtml(animal.owner) + ')</span>' : '') +
            '</div>' +
            '<span class="species-badge">' + escapeHtml(animal.species) + '</span>' +
            '<span class="animal-age">' + animal.age + ' yr' + (animal.age !== 1 ? 's' : '') + '</span>';

        if (editable) {
            html += '<div class="animal-actions">' +
                '<button class="btn btn-secondary btn-sm" onclick="editAnimal(\'' + animal.id + '\',\'' + escapeHtml(animal.name) + '\',\'' + escapeHtml(animal.species) + '\',' + animal.age + ')">Edit</button>' +
                '<button class="btn btn-danger btn-sm" onclick="deleteAnimal(\'' + animal.id + '\')">Delete</button>' +
                '</div>' +
                '<div class="animal-relations"><h5>Relations</h5>' +
                (rels.length > 0 ? rels.map(r => '<div class="relation-item">' +
                    '<span class="relation-badge relation-' + r.relation + '">' + r.relation + '</span>' +
                    '<span>' + escapeHtml(r.user) + '</span>' +
                    '<button class="btn btn-danger btn-xs" onclick="removeRelation(\'' + animal.id + '\',\'' + escapeHtml(r.user) + '\',\'' + r.relation + '\')">&times;</button>' +
                    '</div>').join('') : '<p class="muted">None</p>') +
                (friends.length > 0 ? '<div class="add-relation-form">' +
                    '<select id="relUser_' + animal.id + '">' + friends.map(f => '<option value="' + f + '">' + f + '</option>').join('') + '</select>' +
                    '<select id="relType_' + animal.id + '"><option value="editor">editor</option><option value="know">know</option></select>' +
                    '<button class="btn btn-primary btn-xs" onclick="addRelation(\'' + animal.id + '\')">+</button></div>' : '<p class="muted">Add friends to assign relations</p>') +
                '</div>';
        }
        html += '</div>';
        return html;
    }

    async function createAnimal() {
        const name = document.getElementById('animalName').value.trim();
        const species = document.getElementById('animalSpecies').value.trim();
        const age = parseInt(document.getElementById('animalAge').value) || 0;
        if (!name) { showToast('Name is required', 'error'); return; }
        try {
            await api('/create', { method: 'POST', body: JSON.stringify({ name, species, age }) });
            showToast('Animal created!');
            render();
        } catch (e) { showToast(e.message, 'error'); }
    }

    async function editAnimal(id, name, species, age) {
        const newName = prompt('Name:', name);
        if (newName === null) return;
        const newSpecies = prompt('Species:', species);
        if (newSpecies === null) return;
        const newAge = prompt('Age:', age);
        if (newAge === null) return;
        try {
            await api('/' + id, { method: 'PUT', body: JSON.stringify({ name: newName, species: newSpecies, age: parseInt(newAge) || 0 }) });
            showToast('Animal updated!');
            render();
        } catch (e) { showToast(e.message, 'error'); }
    }

    async function deleteAnimal(id) {
        if (!confirm('Delete this animal?')) return;
        try {
            await api('/' + id, { method: 'DELETE' });
            showToast('Animal deleted');
            render();
        } catch (e) { showToast(e.message, 'error'); }
    }

    async function addRelation(animalId) {
        const u = document.getElementById('relUser_' + animalId);
        const t = document.getElementById('relType_' + animalId);
        if (!u || !t) return;
        try {
            await api('/' + animalId + '/relations', { method: 'POST', body: JSON.stringify({ targetUser: u.value, relation: t.value }) });
            showToast('Relation added!');
            render();
        } catch (e) { showToast(e.message, 'error'); }
    }

    async function removeRelation(animalId, targetUser, relation) {
        try {
            await api('/' + animalId + '/relations', { method: 'DELETE', body: JSON.stringify({ targetUser, relation }) });
            showToast('Relation removed');
            render();
        } catch (e) { showToast(e.message, 'error'); }
    }

    async function sendFriendRequest() {
        const input = document.getElementById('friendTarget');
        const to = input ? input.value.trim() : '';
        if (!to) return;
        try {
            await api('/friends/request', { method: 'POST', body: JSON.stringify({ to }) });
            showToast('Request sent!');
            render();
        } catch (e) { showToast(e.message, 'error'); }
    }

    async function acceptFriend(id) {
        try {
            await api('/friends/' + id + '/accept', { method: 'POST' });
            showToast('Friend accepted!');
            render();
        } catch (e) { showToast(e.message, 'error'); }
    }

    async function denyFriend(id) {
        try {
            await api('/friends/' + id + '/deny', { method: 'POST' });
            showToast('Request denied');
            render();
        } catch (e) { showToast(e.message, 'error'); }
    }

    async function removeFriend(userId) {
        if (!confirm('Remove ' + userId + ' as friend?')) return;
        try {
            await api('/friends/' + userId, { method: 'DELETE' });
            showToast('Friend removed');
            render();
        } catch (e) { showToast(e.message, 'error'); }
    }

    function toggleDebug() {
        const c = document.getElementById('debugContent');
        const a = document.getElementById('debugArrow');
        if (c.style.display === 'none') { c.style.display = 'block'; a.innerHTML = '&#9660;'; refreshDebug(); }
        else { c.style.display = 'none'; a.innerHTML = '&#9654;'; }
    }

    async function refreshDebug() {
        try {
            const data = await api('/debug/tuples');
            const tbody = document.getElementById('debugBody');
            if (!tbody) return;
            const tuples = data.tuples || [];
            tbody.innerHTML = tuples.length === 0 ? '<tr><td colspan="3" class="muted">No tuples</td></tr>' :
                tuples.map(t => '<tr><td>' + escapeHtml(t.user) + '</td><td>' + escapeHtml(t.relation) + '</td><td>' + escapeHtml(t.object) + '</td></tr>').join('');
        } catch (e) { showToast('Error loading tuples', 'error'); }
    }

    function simpleMarkdown(text) {
        return text
            .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
            .replace(/^#### (.+)$/gm, '<h4>$1</h4>')
            .replace(/^### (.+)$/gm, '<h3>$1</h3>')
            .replace(/^## (.+)$/gm, '<h2>$1</h2>')
            .replace(/^# (.+)$/gm, '<h1>$1</h1>')
            .replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
            .replace(/\*(.+?)\*/g, '<em>$1</em>')
            .replace(/` + "`" + `([^` + "`" + `]+)` + "`" + `/g, '<code>$1</code>')
            .replace(/^\s*[-*] (.+)$/gm, '<li>$1</li>')
            .replace(/(<li>.*<\/li>)/s, '<ul>$1</ul>')
            .replace(/\n{2,}/g, '<br><br>')
            .replace(/\n/g, '<br>');
    }

    async function requestAIExplanation() {
        const btn = document.getElementById('aiExplainBtn');
        const resultDiv = document.getElementById('aiExplainResult');
        if (!btn || !resultDiv) return;

        btn.disabled = true;
        btn.innerHTML = '<span class="ai-explain-spinner"></span>Analyzing...';
        resultDiv.innerHTML = '';

        try {
            const [animalsData, friendsData] = await Promise.all([
                api('/list'),
                api('/friends')
            ]);

            const allAnimals = animalsData.animals || [];
            const myAnimals = allAnimals.filter(a => a.owner === currentUser);
            const sharedAnimals = allAnimals.filter(a => a.owner !== currentUser);
            const friends = friendsData.friends || [];

            const res = await fetch('/manager/api/explain-authz', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    user: currentUser,
                    visibleAnimals: allAnimals.map(a => ({ name: a.name, species: a.species, owner: a.owner, canEdit: a.canEdit })),
                    friends: friends,
                    myAnimalsCount: myAnimals.length,
                    sharedAnimalsCount: sharedAnimals.length
                })
            });

            const data = await res.json();
            if (!res.ok) throw new Error(data.error || 'AI Manager request failed');

            resultDiv.innerHTML = '<div class="ai-explain-result">' + simpleMarkdown(data.explanation) + '</div>';
        } catch (e) {
            resultDiv.innerHTML = '<p class="ai-explain-error">Error: ' + escapeHtml(e.message) + '</p>';
        } finally {
            btn.disabled = false;
            btn.textContent = 'Explain My Authorization';
        }
    }

    render();
    </script>
</body>
</html>`

var animalsTmpl = template.Must(template.New("animals").Parse(animalsPageTemplate))

// ──────────────────────────────────────
// Animals API handlers
// ──────────────────────────────────────

var assignableRelations = []string{"owner", "editor", "know"}

func handleAnimalsList(w http.ResponseWriter, r *http.Request) {
	if !fgaReady {
		jsonError(w, "OpenFGA not ready", 503)
		return
	}
	user := getUser(r)
	visibleIds := fgaListObjects("user:"+user, "viewer", "animal")

	type animalResp struct {
		Id        string     `json:"id"`
		Name      string     `json:"name"`
		Species   string     `json:"species"`
		Age       int        `json:"age"`
		Owner     string     `json:"owner"`
		CanEdit   bool       `json:"canEdit"`
		Relations []Relation `json:"relations,omitempty"`
	}

	dataMu.RLock()
	var animals []animalResp
	for _, obj := range visibleIds {
		id := strings.TrimPrefix(obj, "animal:")
		a, ok := dataStore.Animals[id]
		if !ok {
			continue
		}
		canEdit := fgaCheck("user:"+user, "editor", "animal:"+id)
		animals = append(animals, animalResp{
			Id: id, Name: a.Name, Species: a.Species, Age: a.Age,
			Owner: a.Owner, CanEdit: canEdit, Relations: a.Relations,
		})
	}
	dataMu.RUnlock()
	if animals == nil {
		animals = []animalResp{}
	}
	jsonResponse(w, map[string]interface{}{"animals": animals}, 200)
}

func handleAnimalsCreate(w http.ResponseWriter, r *http.Request) {
	if !fgaReady {
		jsonError(w, "OpenFGA not ready", 503)
		return
	}
	user := getUser(r)
	body, err := readBody(r)
	if err != nil {
		jsonError(w, "Invalid request body", 400)
		return
	}
	name := getString(body, "name")
	if name == "" {
		jsonError(w, "Name is required", 400)
		return
	}
	species := getString(body, "species")
	if species == "" {
		species = "Unknown"
	}
	age := getInt(body, "age")

	id := randId()
	animal := &Animal{Name: name, Species: species, Age: age, Owner: user}
	dataMu.Lock()
	dataStore.Animals[id] = animal
	dataMu.Unlock()
	saveData()

	err = fgaWrite([]tupleKey{{User: "user:" + user, Relation: "owner", Object: "animal:" + id}}, nil)
	if err != nil {
		dataMu.Lock()
		delete(dataStore.Animals, id)
		dataMu.Unlock()
		saveData()
		jsonError(w, err.Error(), 500)
		return
	}
	jsonResponse(w, map[string]interface{}{"id": id, "name": name, "species": species, "age": age, "owner": user}, 200)
}

func handleAnimalsUpdate(w http.ResponseWriter, r *http.Request, id string) {
	if !fgaReady {
		jsonError(w, "OpenFGA not ready", 503)
		return
	}
	user := getUser(r)
	animal, ok := dataStore.Animals[id]
	if !ok {
		jsonError(w, "Animal not found", 404)
		return
	}
	if !fgaCheck("user:"+user, "editor", "animal:"+id) {
		jsonError(w, "Not authorized to edit this animal", 403)
		return
	}
	body, err := readBody(r)
	if err != nil {
		jsonError(w, "Invalid request body", 400)
		return
	}
	if v := getString(body, "name"); v != "" {
		animal.Name = v
	}
	if v := getString(body, "species"); v != "" {
		animal.Species = v
	}
	if _, ok := body["age"]; ok {
		animal.Age = getInt(body, "age")
	}
	saveData()
	jsonResponse(w, map[string]interface{}{"id": id, "name": animal.Name, "species": animal.Species, "age": animal.Age, "owner": animal.Owner}, 200)
}

func handleAnimalsDelete(w http.ResponseWriter, r *http.Request, id string) {
	if !fgaReady {
		jsonError(w, "OpenFGA not ready", 503)
		return
	}
	user := getUser(r)
	animal, ok := dataStore.Animals[id]
	if !ok {
		jsonError(w, "Animal not found", 404)
		return
	}
	if !fgaCheck("user:"+user, "editor", "animal:"+id) {
		jsonError(w, "Not authorized to delete this animal", 403)
		return
	}
	deletes := []tupleKey{{User: "user:" + animal.Owner, Relation: "owner", Object: "animal:" + id}}
	if animal.ParentId != "" {
		deletes = append(deletes, tupleKey{User: "animal:" + animal.ParentId, Relation: "parent", Object: "animal:" + id})
	}
	for _, rel := range animal.Relations {
		deletes = append(deletes, tupleKey{User: "user:" + rel.User, Relation: rel.Relation, Object: "animal:" + id})
	}
	fgaWrite(nil, deletes)
	dataMu.Lock()
	delete(dataStore.Animals, id)
	dataMu.Unlock()
	saveData()
	jsonResponse(w, map[string]bool{"success": true}, 200)
}

func handleAnimalsRelationsGet(w http.ResponseWriter, r *http.Request, id string) {
	if !fgaReady {
		jsonError(w, "OpenFGA not ready", 503)
		return
	}
	user := getUser(r)
	animal, ok := dataStore.Animals[id]
	if !ok {
		jsonError(w, "Animal not found", 404)
		return
	}
	if !fgaCheck("user:"+user, "editor", "animal:"+id) {
		jsonError(w, "Not authorized", 403)
		return
	}
	rels := animal.Relations
	if rels == nil {
		rels = []Relation{}
	}
	jsonResponse(w, map[string]interface{}{"relations": rels}, 200)
}

func handleAnimalsRelationsAdd(w http.ResponseWriter, r *http.Request, id string) {
	if !fgaReady {
		jsonError(w, "OpenFGA not ready", 503)
		return
	}
	user := getUser(r)
	animal, ok := dataStore.Animals[id]
	if !ok {
		jsonError(w, "Animal not found", 404)
		return
	}
	body, err := readBody(r)
	if err != nil {
		jsonError(w, "Invalid request body", 400)
		return
	}
	targetUser := getString(body, "targetUser")
	relation := getString(body, "relation")
	if targetUser == "" || relation == "" {
		jsonError(w, "targetUser and relation are required", 400)
		return
	}
	if !contains(assignableRelations, relation) {
		jsonError(w, "Invalid relation", 400)
		return
	}
	if !fgaCheck("user:"+user, "editor", "animal:"+id) {
		jsonError(w, "Not authorized to manage relations on this animal", 403)
		return
	}
	userFriends := dataStore.Friends[user]
	if !contains(userFriends, targetUser) {
		jsonError(w, targetUser+" is not your friend. You can only assign relations to friends.", 400)
		return
	}
	for _, rel := range animal.Relations {
		if rel.User == targetUser && rel.Relation == relation {
			jsonError(w, "Relation already exists", 400)
			return
		}
	}
	err = fgaWrite([]tupleKey{{User: "user:" + targetUser, Relation: relation, Object: "animal:" + id}}, nil)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	animal.Relations = append(animal.Relations, Relation{User: targetUser, Relation: relation})
	saveData()
	jsonResponse(w, map[string]bool{"success": true}, 200)
}

func handleAnimalsRelationsDelete(w http.ResponseWriter, r *http.Request, id string) {
	if !fgaReady {
		jsonError(w, "OpenFGA not ready", 503)
		return
	}
	user := getUser(r)
	animal, ok := dataStore.Animals[id]
	if !ok {
		jsonError(w, "Animal not found", 404)
		return
	}
	body, err := readBody(r)
	if err != nil {
		jsonError(w, "Invalid request body", 400)
		return
	}
	targetUser := getString(body, "targetUser")
	relation := getString(body, "relation")
	if targetUser == "" || relation == "" {
		jsonError(w, "targetUser and relation are required", 400)
		return
	}
	if !fgaCheck("user:"+user, "editor", "animal:"+id) {
		jsonError(w, "Not authorized", 403)
		return
	}
	fgaWrite(nil, []tupleKey{{User: "user:" + targetUser, Relation: relation, Object: "animal:" + id}})
	var newRels []Relation
	for _, rel := range animal.Relations {
		if !(rel.User == targetUser && rel.Relation == relation) {
			newRels = append(newRels, rel)
		}
	}
	animal.Relations = newRels
	saveData()
	jsonResponse(w, map[string]bool{"success": true}, 200)
}

func handleFriendsList(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	userFriends := dataStore.Friends[user]
	if userFriends == nil {
		userFriends = []string{}
	}
	var incoming, outgoing []FriendRequest
	for _, req := range dataStore.FriendRequests {
		if req.To == user && req.Status == "pending" {
			incoming = append(incoming, req)
		}
		if req.From == user && req.Status == "pending" {
			outgoing = append(outgoing, req)
		}
	}
	if incoming == nil {
		incoming = []FriendRequest{}
	}
	if outgoing == nil {
		outgoing = []FriendRequest{}
	}
	jsonResponse(w, map[string]interface{}{"friends": userFriends, "incoming": incoming, "outgoing": outgoing}, 200)
}

func handleFriendsRequest(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	body, err := readBody(r)
	if err != nil {
		jsonError(w, "Invalid request body", 400)
		return
	}
	to := getString(body, "to")
	if to == "" || to == user {
		jsonError(w, "Invalid target user", 400)
		return
	}
	if contains(dataStore.Friends[user], to) {
		jsonError(w, "Already friends", 400)
		return
	}
	for _, req := range dataStore.FriendRequests {
		if ((req.From == user && req.To == to) || (req.From == to && req.To == user)) && req.Status == "pending" {
			jsonError(w, "Request already pending", 400)
			return
		}
	}
	id := randId()
	dataMu.Lock()
	dataStore.FriendRequests = append(dataStore.FriendRequests, FriendRequest{Id: id, From: user, To: to, Status: "pending"})
	dataMu.Unlock()
	saveData()
	jsonResponse(w, map[string]interface{}{"success": true, "id": id}, 200)
}

func handleFriendsAccept(w http.ResponseWriter, r *http.Request, reqId string) {
	if !fgaReady {
		jsonError(w, "OpenFGA not ready", 503)
		return
	}
	user := getUser(r)
	var found *FriendRequest
	for i := range dataStore.FriendRequests {
		if dataStore.FriendRequests[i].Id == reqId {
			found = &dataStore.FriendRequests[i]
			break
		}
	}
	if found == nil {
		jsonError(w, "Request not found", 404)
		return
	}
	if found.To != user {
		jsonError(w, "Not your request to accept", 403)
		return
	}
	if found.Status != "pending" {
		jsonError(w, "Request already handled", 400)
		return
	}
	dataMu.Lock()
	found.Status = "accepted"
	if dataStore.Friends[user] == nil {
		dataStore.Friends[user] = []string{}
	}
	if dataStore.Friends[found.From] == nil {
		dataStore.Friends[found.From] = []string{}
	}
	dataStore.Friends[user] = append(dataStore.Friends[user], found.From)
	dataStore.Friends[found.From] = append(dataStore.Friends[found.From], user)
	dataMu.Unlock()
	saveData()

	fgaWrite([]tupleKey{
		{User: "user:" + found.From, Relation: "friend", Object: "user:" + user},
		{User: "user:" + user, Relation: "friend", Object: "user:" + found.From},
	}, nil)

	jsonResponse(w, map[string]bool{"success": true}, 200)
}

func handleFriendsDeny(w http.ResponseWriter, r *http.Request, reqId string) {
	user := getUser(r)
	for i := range dataStore.FriendRequests {
		if dataStore.FriendRequests[i].Id == reqId {
			if dataStore.FriendRequests[i].To != user {
				jsonError(w, "Not your request to deny", 403)
				return
			}
			dataStore.FriendRequests[i].Status = "denied"
			saveData()
			jsonResponse(w, map[string]bool{"success": true}, 200)
			return
		}
	}
	jsonError(w, "Request not found", 404)
}

func handleFriendsRemove(w http.ResponseWriter, r *http.Request, userId string) {
	if !fgaReady {
		jsonError(w, "OpenFGA not ready", 503)
		return
	}
	user := getUser(r)
	dataMu.Lock()
	if friends, ok := dataStore.Friends[user]; ok {
		var filtered []string
		for _, f := range friends {
			if f != userId {
				filtered = append(filtered, f)
			}
		}
		dataStore.Friends[user] = filtered
	}
	if friends, ok := dataStore.Friends[userId]; ok {
		var filtered []string
		for _, f := range friends {
			if f != user {
				filtered = append(filtered, f)
			}
		}
		dataStore.Friends[userId] = filtered
	}
	dataMu.Unlock()
	saveData()

	fgaWrite(nil, []tupleKey{
		{User: "user:" + userId, Relation: "friend", Object: "user:" + user},
		{User: "user:" + user, Relation: "friend", Object: "user:" + userId},
	})

	jsonResponse(w, map[string]bool{"success": true}, 200)
}

func handleDebugTuples(w http.ResponseWriter, r *http.Request) {
	if !fgaReady {
		jsonError(w, "OpenFGA not ready", 503)
		return
	}
	result, err := fgaRequest("POST", "/stores/"+fgaStoreId+"/read", map[string]interface{}{})
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}
	tuples, _ := result["tuples"].([]interface{})
	var keys []map[string]string
	for _, t := range tuples {
		tm, _ := t.(map[string]interface{})
		key, _ := tm["key"].(map[string]interface{})
		keys = append(keys, map[string]string{
			"user":     fmt.Sprintf("%v", key["user"]),
			"relation": fmt.Sprintf("%v", key["relation"]),
			"object":   fmt.Sprintf("%v", key["object"]),
		})
	}
	if keys == nil {
		keys = []map[string]string{}
	}
	jsonResponse(w, map[string]interface{}{"tuples": keys}, 200)
}

// ──────────────────────────────────────
// Router
// ──────────────────────────────────────

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	externalURL = os.Getenv("EXTERNAL_URL")
	if externalURL == "" {
		externalURL = "http://localhost:8000"
	}
	openfgaURL = os.Getenv("OPENFGA_URL")
	if openfgaURL == "" {
		openfgaURL = "http://openfga:8080"
	}
	auditURL = os.Getenv("AUDIT_URL")
	if auditURL == "" {
		auditURL = "http://ai-manager:5000"
	}

	loadData()
	go loadFGAConfig()

	http.HandleFunc("/public", func(w http.ResponseWriter, r *http.Request) {
		if wantsJSON(r) {
			jsonResponse(w, map[string]interface{}{
				"status": "ok", "message": "Public content - visible to everyone",
				"path": r.URL.Path, "time": time.Now().Format(time.RFC3339),
			}, http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, buildPageData(r, true))
	})

	http.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		keycloakLogout := externalURL + "/login/realms/AuthorizationRealm/protocol/openid-connect/logout" +
			"?client_id=envoy" +
			"&post_logout_redirect_uri=" + url.QueryEscape(externalURL+"/signout")
		http.Redirect(w, r, keycloakLogout, http.StatusFound)
	})

	http.HandleFunc("/api/protected", func(w http.ResponseWriter, r *http.Request) {
		user := r.Header.Get("x-current-user")
		metadata := r.Header.Get("x-user-metadata")
		if wantsJSON(r) {
			jsonResponse(w, map[string]interface{}{
				"status": "ok", "message": "Protected content - access granted",
				"user": user, "metadata": metadata,
				"path": r.URL.Path, "method": r.Method, "time": time.Now().Format(time.RFC3339),
			}, http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, buildPageData(r, false))
	})

	http.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		if wantsJSON(r) {
			jsonResponse(w, map[string]interface{}{
				"status": "healthy", "service": "test-app",
				"uptime": time.Since(startTime).String(), "fgaReady": fgaReady,
			}, http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, buildPageData(r, false))
	})

	// Animals page
	http.HandleFunc("/animals", func(w http.ResponseWriter, r *http.Request) {
		user := getUser(r)
		if user == "anonymous" {
			http.Redirect(w, r, "/home", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		animalsTmpl.Execute(w, struct{ Username string }{Username: user})
	})

	// Animals API
	http.HandleFunc("/api/animals/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			handleAnimalsList(w, r)
		}
	})
	http.HandleFunc("/api/animals/create", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			handleAnimalsCreate(w, r)
		}
	})
	http.HandleFunc("/api/animals/friends", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			handleFriendsList(w, r)
		}
	})
	http.HandleFunc("/api/animals/friends/request", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			handleFriendsRequest(w, r)
		}
	})
	http.HandleFunc("/api/animals/debug/tuples", func(w http.ResponseWriter, r *http.Request) {
		handleDebugTuples(w, r)
	})

	// Dynamic routes for /api/animals/friends/:id/accept, /api/animals/friends/:id/deny, /api/animals/friends/:userId (DELETE)
	http.HandleFunc("/api/animals/friends/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/animals/friends/")
		parts := strings.Split(path, "/")

		if len(parts) == 2 && parts[1] == "accept" && r.Method == "POST" {
			handleFriendsAccept(w, r, parts[0])
			return
		}
		if len(parts) == 2 && parts[1] == "deny" && r.Method == "POST" {
			handleFriendsDeny(w, r, parts[0])
			return
		}
		if len(parts) == 1 && r.Method == "DELETE" {
			handleFriendsRemove(w, r, parts[0])
			return
		}
		jsonError(w, "Not found", 404)
	})

	// Dynamic routes for /api/animals/:id, /api/animals/:id/relations
	http.HandleFunc("/api/animals/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/animals/")
		// Skip paths handled by more specific handlers
		if strings.HasPrefix(path, "list") || strings.HasPrefix(path, "create") ||
			strings.HasPrefix(path, "friends") || strings.HasPrefix(path, "debug") ||
			strings.HasPrefix(path, "status") {
			return
		}

		parts := strings.Split(path, "/")
		if len(parts) == 1 && parts[0] != "" {
			id := parts[0]
			switch r.Method {
			case "PUT":
				handleAnimalsUpdate(w, r, id)
			case "DELETE":
				handleAnimalsDelete(w, r, id)
			default:
				jsonError(w, "Method not allowed", 405)
			}
			return
		}
		if len(parts) == 2 && parts[1] == "relations" {
			id := parts[0]
			switch r.Method {
			case "GET":
				handleAnimalsRelationsGet(w, r, id)
			case "POST":
				handleAnimalsRelationsAdd(w, r, id)
			case "DELETE":
				handleAnimalsRelationsDelete(w, r, id)
			default:
				jsonError(w, "Method not allowed", 405)
			}
			return
		}
		jsonError(w, "Not found", 404)
	})

	http.HandleFunc("/api/animals/status", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, map[string]interface{}{"ready": fgaReady, "storeId": fgaStoreId, "modelId": fgaModelId}, 200)
	})

	http.HandleFunc("/home", func(w http.ResponseWriter, r *http.Request) {
		if wantsJSON(r) {
			jsonResponse(w, map[string]interface{}{"status": "ok", "message": "Authorization POC - Test Application"}, http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, buildPageData(r, false))
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/public", http.StatusFound)
			return
		}
		if wantsJSON(r) {
			jsonResponse(w, map[string]string{"status": "error", "message": "Not found", "path": r.URL.Path}, http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "Not found: %s", r.URL.Path)
	})

	log.Printf("Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
