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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var startTime = time.Now()

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
			if json.Unmarshal(data, &cfg) == nil && cfg.StoreId != "" && cfg.ModelId != "" {
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
	json.NewDecoder(resp.Body).Decode(&result)
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
	return err
}

func fgaCheck(user, relation, object string) bool {
	body := map[string]interface{}{
		"tuple_key":              map[string]string{"user": user, "relation": relation, "object": object},
		"authorization_model_id": fgaModelId,
	}
	result, err := fgaRequest("POST", "/stores/"+fgaStoreId+"/check", body)
	if err != nil {
		return false
	}
	allowed, _ := result["allowed"].(bool)
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
		return nil
	}
	objects, ok := result["objects"].([]interface{})
	if !ok {
		return nil
	}
	var out []string
	for _, o := range objects {
		if s, ok := o.(string); ok {
			out = append(out, s)
		}
	}
	return out
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
	dataMu   sync.Mutex
	dataFile = "/data/animals.json"
)

func loadData() {
	data, err := os.ReadFile(dataFile)
	if err != nil {
		return
	}
	dataMu.Lock()
	defer dataMu.Unlock()
	json.Unmarshal(data, dataStore)
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
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/json") {
		return true
	}
	if r.URL.Query().Get("format") == "json" {
		return true
	}
	return false
}

func getUser(r *http.Request) string {
	user := r.Header.Get("x-current-user")
	if user == "" {
		user = "anonymous"
	}
	return user
}

func readBody(r *http.Request) map[string]interface{} {
	var m map[string]interface{}
	json.NewDecoder(r.Body).Decode(&m)
	return m
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
    <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;500;600;700&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg: #0f172a;
            --surface: #1e293b;
            --surface-hover: #273548;
            --border: #334155;
            --text: #e2e8f0;
            --text-muted: #94a3b8;
            --primary: #8b5cf6;
            --secondary: #3b82f6;
            --accent: #22d3ee;
            --success: #10b981;
            --warning: #f59e0b;
            --danger: #ef4444;
        }

        * { box-sizing: border-box; margin: 0; padding: 0; }

        body {
            font-family: 'Outfit', sans-serif;
            background: var(--bg);
            color: var(--text);
            min-height: 100vh;
            background-image:
                radial-gradient(circle at 15% 15%, rgba(139, 92, 246, 0.08) 0%, transparent 40%),
                radial-gradient(circle at 85% 85%, rgba(59, 130, 246, 0.08) 0%, transparent 40%);
        }

        /* Top nav */
        nav {
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 1rem 2rem;
            border-bottom: 1px solid var(--border);
            background: rgba(15, 23, 42, 0.8);
            backdrop-filter: blur(12px);
            position: sticky;
            top: 0;
            z-index: 100;
        }

        .nav-brand {
            display: flex;
            align-items: center;
            gap: 0.75rem;
        }

        .nav-logo {
            width: 36px;
            height: 36px;
            background: linear-gradient(135deg, var(--primary), var(--accent));
            border-radius: 10px;
            display: flex;
            align-items: center;
            justify-content: center;
            font-size: 1.1rem;
            font-weight: 700;
        }

        .nav-title {
            font-size: 1.15rem;
            font-weight: 600;
            background: linear-gradient(to right, var(--primary), var(--accent));
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }

        .nav-links {
            display: flex;
            align-items: center;
            gap: 0.5rem;
        }

        .nav-links a {
            color: var(--text-muted);
            text-decoration: none;
            padding: 0.5rem 1rem;
            border-radius: 8px;
            font-size: 0.9rem;
            font-weight: 500;
            transition: all 0.2s;
        }

        .nav-links a:hover {
            color: var(--text);
            background: var(--surface);
        }

        .nav-links a.active {
            color: var(--accent);
            background: rgba(34, 211, 238, 0.1);
        }

        .nav-user {
            display: flex;
            align-items: center;
            gap: 1rem;
        }

        .user-badge {
            display: flex;
            align-items: center;
            gap: 0.6rem;
            padding: 0.4rem 0.9rem;
            background: var(--surface);
            border: 1px solid var(--border);
            border-radius: 999px;
        }

        .user-avatar {
            width: 28px;
            height: 28px;
            border-radius: 50%;
            background: linear-gradient(135deg, var(--primary), var(--secondary));
            display: flex;
            align-items: center;
            justify-content: center;
            font-size: 0.75rem;
            font-weight: 700;
            color: white;
            text-transform: uppercase;
        }

        .user-name {
            font-size: 0.9rem;
            font-weight: 500;
        }

        .btn-logout {
            display: inline-flex;
            align-items: center;
            gap: 0.4rem;
            padding: 0.5rem 1rem;
            background: rgba(239, 68, 68, 0.1);
            border: 1px solid rgba(239, 68, 68, 0.2);
            color: #fca5a5;
            border-radius: 8px;
            text-decoration: none;
            font-size: 0.85rem;
            font-weight: 500;
            transition: all 0.2s;
            cursor: pointer;
        }

        .btn-logout:hover {
            background: rgba(239, 68, 68, 0.2);
            color: #fecaca;
        }

        /* Main content */
        .container {
            max-width: 960px;
            margin: 0 auto;
            padding: 2rem;
        }

        .page-header {
            margin-bottom: 2rem;
        }

        .page-header h1 {
            font-size: 1.8rem;
            font-weight: 700;
            margin-bottom: 0.5rem;
        }

        .page-header p {
            color: var(--text-muted);
            font-size: 1rem;
        }

        /* Decision box */
        .decision-box {
            border-radius: 16px;
            padding: 1.5rem;
            margin-bottom: 1.5rem;
            border: 1px solid;
            display: flex;
            align-items: flex-start;
            gap: 1rem;
        }

        .decision-box.allowed {
            background: rgba(16, 185, 129, 0.08);
            border-color: rgba(16, 185, 129, 0.25);
        }

        .decision-box.public {
            background: rgba(34, 211, 238, 0.08);
            border-color: rgba(34, 211, 238, 0.25);
        }

        .decision-icon {
            width: 44px;
            height: 44px;
            border-radius: 12px;
            display: flex;
            align-items: center;
            justify-content: center;
            font-size: 1.4rem;
            flex-shrink: 0;
        }

        .decision-box.allowed .decision-icon {
            background: rgba(16, 185, 129, 0.15);
        }

        .decision-box.public .decision-icon {
            background: rgba(34, 211, 238, 0.15);
        }

        .decision-content h3 {
            font-size: 1rem;
            font-weight: 600;
            margin-bottom: 0.3rem;
        }

        .decision-box.allowed .decision-content h3 { color: #6ee7b7; }
        .decision-box.public .decision-content h3 { color: #67e8f9; }

        .decision-content p {
            color: var(--text-muted);
            font-size: 0.9rem;
            line-height: 1.5;
        }

        /* Info grid */
        .info-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
            gap: 1rem;
            margin-bottom: 1.5rem;
        }

        .info-card {
            background: var(--surface);
            border: 1px solid var(--border);
            border-radius: 14px;
            padding: 1.25rem;
            transition: border-color 0.2s;
        }

        .info-card:hover {
            border-color: rgba(255, 255, 255, 0.15);
        }

        .info-card-label {
            font-size: 0.75rem;
            font-weight: 600;
            text-transform: uppercase;
            letter-spacing: 0.05em;
            color: var(--text-muted);
            margin-bottom: 0.5rem;
        }

        .info-card-value {
            font-size: 1.1rem;
            font-weight: 500;
            word-break: break-all;
        }

        .info-card-value.mono {
            font-family: 'Fira Code', 'Courier New', monospace;
            font-size: 0.9rem;
            color: var(--accent);
        }

        /* Role pills */
        .role-pills {
            display: flex;
            flex-wrap: wrap;
            gap: 0.4rem;
        }

        .role-pill {
            padding: 0.25rem 0.7rem;
            border-radius: 999px;
            font-size: 0.8rem;
            font-weight: 500;
            background: rgba(139, 92, 246, 0.15);
            color: #c4b5fd;
            border: 1px solid rgba(139, 92, 246, 0.25);
        }

        /* Endpoint cards */
        .endpoints-section h2 {
            font-size: 1.2rem;
            font-weight: 600;
            margin-bottom: 1rem;
            color: var(--text-muted);
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
            background: var(--surface);
            border: 1px solid var(--border);
            border-radius: 12px;
            text-decoration: none;
            color: var(--text);
            transition: all 0.2s;
        }

        .endpoint-item:hover {
            border-color: var(--primary);
            background: var(--surface-hover);
            transform: translateX(4px);
        }

        .endpoint-method {
            padding: 0.2rem 0.6rem;
            border-radius: 6px;
            font-size: 0.7rem;
            font-weight: 700;
            font-family: 'Fira Code', monospace;
            background: rgba(59, 130, 246, 0.15);
            color: #93c5fd;
            letter-spacing: 0.03em;
        }

        .endpoint-path {
            font-family: 'Fira Code', monospace;
            font-size: 0.9rem;
            color: var(--accent);
            font-weight: 500;
        }

        .endpoint-desc {
            color: var(--text-muted);
            font-size: 0.85rem;
            margin-left: auto;
        }

        .endpoint-lock {
            font-size: 1rem;
            opacity: 0.5;
        }

        /* Footer */
        footer {
            display: flex;
            align-items: center;
            justify-content: space-between;
            padding: 2rem;
            color: var(--text-muted);
            font-size: 0.8rem;
            border-top: 1px solid var(--border);
            margin-top: 2rem;
        }

        footer a {
            color: var(--accent);
            text-decoration: none;
        }

        .btn-rule-builder {
            display: inline-flex;
            align-items: center;
            gap: 0.5rem;
            padding: 0.5rem 1rem;
            background: rgba(139, 92, 246, 0.1);
            border: 1px solid rgba(139, 92, 246, 0.25);
            color: #c4b5fd;
            border-radius: 8px;
            text-decoration: none;
            font-size: 0.85rem;
            font-weight: 500;
            transition: all 0.2s;
            white-space: nowrap;
        }

        .btn-rule-builder:hover {
            background: rgba(139, 92, 246, 0.2);
            color: #ddd6fe;
            border-color: rgba(139, 92, 246, 0.4);
        }

        @media (max-width: 640px) {
            nav { padding: 0.75rem 1rem; flex-wrap: wrap; gap: 0.75rem; }
            .nav-links { display: none; }
            .container { padding: 1.25rem; }
            .info-grid { grid-template-columns: 1fr; }
            .endpoint-desc { display: none; }
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
            <a href="/"{{if eq .Path "/"}} class="active"{{end}}>Home</a>
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
                <a href="/" class="btn-logout" style="background: rgba(139,92,246,0.1); border-color: rgba(139,92,246,0.2); color: #c4b5fd;">Sign in</a>
            {{end}}
        </div>
    </nav>

    <div class="container">
        {{if eq .Path "/"}}
            <div class="page-header">
                <h1>Fine-Grained Authorization</h1>
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
                            {{if not .RoleList}}<span style="color: var(--text-muted); font-size: 0.9rem;">No roles assigned</span>{{end}}
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
                        <span class="endpoint-lock">&#x1F513;</span>
                    </a>
                    <a href="/api/protected" class="endpoint-item">
                        <span class="endpoint-method">GET</span>
                        <span class="endpoint-path">/api/protected</span>
                        <span class="endpoint-desc">Requires valid Bearer token</span>
                        <span class="endpoint-lock">&#x1F512;</span>
                    </a>
                    <a href="/animals" class="endpoint-item">
                        <span class="endpoint-method">GET</span>
                        <span class="endpoint-path">/animals</span>
                        <span class="endpoint-desc">Animals demo with OpenFGA ReBAC</span>
                        <span class="endpoint-lock">&#x1F512;</span>
                    </a>
                    <a href="/api/health" class="endpoint-item">
                        <span class="endpoint-method">GET</span>
                        <span class="endpoint-path">/api/health</span>
                        <span class="endpoint-desc">Service health check</span>
                        <span class="endpoint-lock">&#x1F512;</span>
                    </a>
                </div>
            </div>

        {{else if .IsPublic}}
            <div class="page-header">
                <h1>Public Resource</h1>
                <p>This content is accessible to everyone without authentication.</p>
            </div>

            <div class="decision-box public">
                <div class="decision-icon">{{.StatusIcon}}</div>
                <div class="decision-content">
                    <h3>Public Access &mdash; No Auth Required</h3>
                    <p>OPA evaluated the request path <code>/public</code> and granted access via the <strong>pass-through rule</strong>. No token validation was performed. Envoy's OAuth2 filter also skips this path.</p>
                </div>
            </div>

            <div class="info-grid">
                <div class="info-card">
                    <div class="info-card-label">OPA Decision</div>
                    <div class="info-card-value mono">{{.Decision}}</div>
                </div>
                <div class="info-card">
                    <div class="info-card-label">Path</div>
                    <div class="info-card-value mono">{{.Path}}</div>
                </div>
                <div class="info-card">
                    <div class="info-card-label">Server Time</div>
                    <div class="info-card-value mono">{{.Time}}</div>
                </div>
            </div>

        {{else}}
            <div class="page-header">
                <h1>{{if eq .Path "/api/protected"}}Protected Resource{{else if eq .Path "/api/health"}}Service Health{{else}}{{.Path}}{{end}}</h1>
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
                            {{if not .RoleList}}<span style="color: var(--text-muted); font-size: 0.9rem;">No roles assigned</span>{{end}}
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
        <a href="http://localhost:5001" class="btn-rule-builder" target="_blank">Go to AuthZ Rule Builder &#8594;</a>
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
    <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;500;600;700&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg: #0f172a; --surface: #1e293b; --surface-hover: #273548;
            --border: #334155; --text: #e2e8f0; --text-muted: #94a3b8;
            --primary: #8b5cf6; --secondary: #3b82f6; --accent: #22d3ee;
            --success: #10b981; --warning: #f59e0b; --danger: #ef4444;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body { font-family: 'Outfit', sans-serif; background: var(--bg); color: var(--text); min-height: 100vh;
            background-image: radial-gradient(circle at 15% 15%, rgba(139,92,246,0.08) 0%, transparent 40%),
            radial-gradient(circle at 85% 85%, rgba(59,130,246,0.08) 0%, transparent 40%); }

        nav { display: flex; align-items: center; justify-content: space-between; padding: 1rem 2rem;
            border-bottom: 1px solid var(--border); background: rgba(15,23,42,0.8); backdrop-filter: blur(12px);
            position: sticky; top: 0; z-index: 100; }
        .nav-brand { display: flex; align-items: center; gap: 0.75rem; }
        .nav-logo { width: 36px; height: 36px; background: linear-gradient(135deg, var(--primary), var(--accent));
            border-radius: 10px; display: flex; align-items: center; justify-content: center; font-size: 1.1rem; font-weight: 700; }
        .nav-title { font-size: 1.15rem; font-weight: 600; background: linear-gradient(to right, var(--primary), var(--accent));
            -webkit-background-clip: text; -webkit-text-fill-color: transparent; }
        .nav-links { display: flex; align-items: center; gap: 0.5rem; }
        .nav-links a { color: var(--text-muted); text-decoration: none; padding: 0.5rem 1rem; border-radius: 8px;
            font-size: 0.9rem; font-weight: 500; transition: all 0.2s; }
        .nav-links a:hover { color: var(--text); background: var(--surface); }
        .nav-links a.active { color: var(--accent); background: rgba(34,211,238,0.1); }
        .nav-user { display: flex; align-items: center; gap: 1rem; }
        .user-badge { display: flex; align-items: center; gap: 0.6rem; padding: 0.4rem 0.9rem;
            background: var(--surface); border: 1px solid var(--border); border-radius: 999px; }
        .user-avatar { width: 28px; height: 28px; border-radius: 50%;
            background: linear-gradient(135deg, var(--primary), var(--secondary));
            display: flex; align-items: center; justify-content: center; font-size: 0.75rem; font-weight: 700; color: white; text-transform: uppercase; }
        .user-name { font-size: 0.9rem; font-weight: 500; }
        .btn-logout { display: inline-flex; align-items: center; gap: 0.4rem; padding: 0.5rem 1rem;
            background: rgba(239,68,68,0.1); border: 1px solid rgba(239,68,68,0.2); color: #fca5a5;
            border-radius: 8px; text-decoration: none; font-size: 0.85rem; font-weight: 500; transition: all 0.2s; }
        .btn-logout:hover { background: rgba(239,68,68,0.2); color: #fecaca; }

        .container { max-width: 960px; margin: 0 auto; padding: 2rem; }
        .page-header { margin-bottom: 2rem; }
        .page-header h1 { font-size: 1.8rem; font-weight: 700; margin-bottom: 0.5rem; }
        .page-header p { color: var(--text-muted); font-size: 1rem; }

        .card { background: rgba(30,41,59,0.7); backdrop-filter: blur(10px); border: 1px solid rgba(255,255,255,0.1);
            border-radius: 16px; padding: 1.5rem; margin-bottom: 1.5rem; }
        .card h3 { color: var(--accent); margin-bottom: 0.75rem; font-size: 1.1rem; }
        .card h4 { color: #94a3b8; font-size: 0.85rem; margin-top: 0.75rem; margin-bottom: 0.25rem; }

        .top-row { display: grid; grid-template-columns: 1fr 1fr; gap: 1.5rem; margin-bottom: 1.5rem; }
        .friends-list { margin-bottom: 0.75rem; }
        .friend-item { display: flex; align-items: center; gap: 0.5rem; padding: 0.4rem 0; }
        .friend-request-form { display: flex; gap: 0.5rem; margin-top: 0.75rem; }

        input[type="text"], input[type="number"], select {
            width: 100%; padding: 0.5rem 0.75rem; margin-bottom: 0.5rem; border-radius: 6px;
            border: 1px solid rgba(255,255,255,0.1); background: rgba(0,0,0,0.3); color: white;
            font-family: 'Outfit', sans-serif; font-size: 0.95rem; }
        input:focus, select:focus { outline: none; border-color: var(--primary); }

        .friend-request-form select { flex: 1; margin-bottom: 0; }
        .friend-request-form input { flex: 1; margin-bottom: 0; }

        .btn { padding: 0.5rem 1rem; border-radius: 8px; font-weight: 600; cursor: pointer; border: none; transition: all 0.2s; font-family: 'Outfit', sans-serif; }
        .btn-primary { background: linear-gradient(135deg, var(--primary), var(--secondary)); color: white; }
        .btn-primary:hover { box-shadow: 0 0 20px rgba(139,92,246,0.4); }
        .btn-danger { background: rgba(239,68,68,0.15); color: #fca5a5; border: 1px solid rgba(239,68,68,0.3); }
        .btn-danger:hover { background: rgba(239,68,68,0.3); }
        .btn-success { background: rgba(16,185,129,0.15); color: #6ee7b7; border: 1px solid rgba(16,185,129,0.3); }
        .btn-success:hover { background: rgba(16,185,129,0.3); }
        .btn-secondary { background: rgba(255,255,255,0.1); color: white; border: 1px solid rgba(255,255,255,0.1); }
        .btn-sm { padding: 0.35rem 0.75rem; font-size: 0.8rem; }
        .btn-xs { padding: 0.2rem 0.5rem; font-size: 0.75rem; line-height: 1; }

        .animals-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 1rem; margin-top: 0.75rem; }
        .animal-card { background: rgba(0,0,0,0.25); border: 1px solid rgba(255,255,255,0.08); border-radius: 12px; padding: 1rem; }
        .animal-card-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem; }
        .animal-owner { color: #94a3b8; font-size: 0.8rem; }
        .species-badge { display: inline-block; padding: 0.15rem 0.6rem; border-radius: 999px; font-size: 0.75rem;
            font-weight: 600; background: rgba(139,92,246,0.15); color: #c4b5fd; margin-right: 0.5rem; }
        .animal-age { color: #94a3b8; font-size: 0.85rem; }
        .animal-actions { margin-top: 0.75rem; display: flex; gap: 0.5rem; }
        .animal-relations { margin-top: 0.75rem; padding-top: 0.75rem; border-top: 1px solid rgba(255,255,255,0.06); }
        .animal-relations h5 { color: #94a3b8; font-size: 0.8rem; margin-bottom: 0.4rem; text-transform: uppercase; letter-spacing: 0.05em; }
        .relation-item { display: flex; align-items: center; gap: 0.4rem; padding: 0.2rem 0; font-size: 0.85rem; }
        .relation-badge { display: inline-block; padding: 0.1rem 0.5rem; border-radius: 999px; font-size: 0.7rem; font-weight: 600; text-transform: uppercase; }
        .relation-owner { background: rgba(251,191,36,0.15); color: #fcd34d; }
        .relation-editor { background: rgba(59,130,246,0.15); color: #93c5fd; }
        .relation-know { background: rgba(16,185,129,0.15); color: #6ee7b7; }
        .add-relation-form { display: flex; gap: 0.35rem; margin-top: 0.4rem; }
        .add-relation-form select { flex: 1; margin-bottom: 0; padding: 0.25rem 0.4rem; font-size: 0.75rem; }
        .add-relation-form select:last-of-type { width: 65px; flex: none; }

        .muted { color: #64748b; font-size: 0.85rem; }

        .debug-section { margin-top: 1rem; }
        .debug-toggle { cursor: pointer; user-select: none; color: #94a3b8; font-size: 0.9rem; }
        .debug-table { width: 100%; border-collapse: collapse; margin-top: 0.75rem; font-size: 0.85rem; }
        .debug-table th, .debug-table td { text-align: left; padding: 0.5rem 0.75rem; border-bottom: 1px solid rgba(255,255,255,0.06); }
        .debug-table th { color: var(--accent); font-weight: 600; }
        .debug-table td { color: #cbd5e1; font-family: 'Fira Code', monospace; font-size: 0.8rem; }

        .toast { position: fixed; bottom: 2rem; right: 2rem; padding: 1rem 1.5rem; border-radius: 12px;
            color: white; font-weight: 600; z-index: 1000; animation: slideIn 0.3s ease, fadeOut 0.3s ease 2.7s forwards; }
        .toast-success { background: linear-gradient(135deg, #10b981, #059669); }
        .toast-error { background: linear-gradient(135deg, #ef4444, #dc2626); }
        @keyframes slideIn { from { transform: translateX(100%); opacity: 0; } to { transform: translateX(0); opacity: 1; } }
        @keyframes fadeOut { from { opacity: 1; } to { opacity: 0; } }

        footer { display: flex; align-items: center; justify-content: space-between; padding: 2rem;
            color: var(--text-muted); font-size: 0.8rem; border-top: 1px solid var(--border); margin-top: 2rem; }
        footer a { color: var(--accent); text-decoration: none; }

        .ai-explain-box { background: rgba(30,41,59,0.7); backdrop-filter: blur(10px);
            border: 1px solid rgba(139,92,246,0.3); border-radius: 16px; padding: 1.5rem; margin-bottom: 1.5rem; }
        .ai-explain-box h3 { color: #c4b5fd; margin-bottom: 0.5rem; font-size: 1.1rem; }
        .ai-explain-box p.desc { color: #94a3b8; font-size: 0.9rem; margin-bottom: 1rem; }
        .ai-explain-btn { padding: 0.6rem 1.25rem; border-radius: 10px; font-weight: 600; cursor: pointer; border: none;
            background: linear-gradient(135deg, #8b5cf6, #6d28d9); color: white; font-family: 'Outfit', sans-serif;
            font-size: 0.95rem; transition: all 0.2s; }
        .ai-explain-btn:hover { box-shadow: 0 0 25px rgba(139,92,246,0.5); transform: translateY(-1px); }
        .ai-explain-btn:disabled { opacity: 0.6; cursor: not-allowed; transform: none; box-shadow: none; }
        .ai-explain-spinner { display: inline-block; width: 16px; height: 16px; border: 2px solid rgba(255,255,255,0.3);
            border-top-color: white; border-radius: 50%; animation: aispin 0.6s linear infinite; margin-right: 0.5rem; vertical-align: middle; }
        @keyframes aispin { to { transform: rotate(360deg); } }
        .ai-explain-result { margin-top: 1rem; padding: 1.25rem; background: rgba(0,0,0,0.3); border: 1px solid rgba(255,255,255,0.08);
            border-radius: 12px; line-height: 1.7; font-size: 0.92rem; color: #e2e8f0; }
        .ai-explain-result h1, .ai-explain-result h2, .ai-explain-result h3, .ai-explain-result h4 {
            color: #c4b5fd; margin: 1rem 0 0.5rem 0; }
        .ai-explain-result h1 { font-size: 1.2rem; } .ai-explain-result h2 { font-size: 1.1rem; }
        .ai-explain-result h3 { font-size: 1rem; } .ai-explain-result h4 { font-size: 0.95rem; }
        .ai-explain-result ul, .ai-explain-result ol { padding-left: 1.5rem; margin: 0.5rem 0; }
        .ai-explain-result li { margin-bottom: 0.3rem; }
        .ai-explain-result code { background: rgba(139,92,246,0.15); padding: 0.15rem 0.4rem; border-radius: 4px;
            font-family: 'Fira Code', monospace; font-size: 0.85rem; color: #c4b5fd; }
        .ai-explain-result strong { color: #e9d5ff; }
        .ai-explain-error { color: #fca5a5; margin-top: 1rem; }

        @media (max-width: 640px) {
            nav { padding: 0.75rem 1rem; flex-wrap: wrap; gap: 0.75rem; }
            .nav-links { display: none; }
            .container { padding: 1.25rem; }
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
            <a href="/">Home</a>
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
            <h1>Animals</h1>
            <p>Manage your animals with OpenFGA relationship-based access control. Logged in as <strong>{{.Username}}</strong>.</p>
        </div>

        <div id="app">Loading...</div>
    </div>

    <footer>
        <span>Fine-Grained Authorization POC</span>
        <a href="http://localhost:5001" target="_blank">AuthZ Rule Builder &rarr;</a>
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

            const res = await fetch('http://localhost:5001/api/explain-authz', {
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
	body := readBody(r)
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
	dataStore.Animals[id] = animal
	saveData()

	err := fgaWrite([]tupleKey{{User: "user:" + user, Relation: "owner", Object: "animal:" + id}}, nil)
	if err != nil {
		delete(dataStore.Animals, id)
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
	body := readBody(r)
	if v := getString(body, "name"); v != "" {
		animal.Name = v
	}
	if v := getString(body, "species"); v != "" {
		animal.Species = v
	}
	if v, ok := body["age"]; ok {
		animal.Age = getInt(body, "age")
		_ = v
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
	delete(dataStore.Animals, id)
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
	body := readBody(r)
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
	err := fgaWrite([]tupleKey{{User: "user:" + targetUser, Relation: relation, Object: "animal:" + id}}, nil)
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
	body := readBody(r)
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
	body := readBody(r)
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
	dataStore.FriendRequests = append(dataStore.FriendRequests, FriendRequest{Id: id, From: user, To: to, Status: "pending"})
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
	found.Status = "accepted"
	if dataStore.Friends[user] == nil {
		dataStore.Friends[user] = []string{}
	}
	if dataStore.Friends[found.From] == nil {
		dataStore.Friends[found.From] = []string{}
	}
	dataStore.Friends[user] = append(dataStore.Friends[user], found.From)
	dataStore.Friends[found.From] = append(dataStore.Friends[found.From], user)
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
	rand.Seed(time.Now().UnixNano())

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	openfgaURL = os.Getenv("OPENFGA_URL")
	if openfgaURL == "" {
		openfgaURL = "http://openfga:8080"
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
		keycloakLogout := "http://localhost:8080/realms/myrealm/protocol/openid-connect/logout" +
			"?client_id=envoy" +
			"&post_logout_redirect_uri=http%3A%2F%2Flocalhost%3A8000%2Fsignout"
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
			http.Redirect(w, r, "/", http.StatusFound)
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

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			if wantsJSON(r) {
				jsonResponse(w, map[string]string{"status": "error", "message": "Not found", "path": r.URL.Path}, http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, "Not found: %s", r.URL.Path)
			return
		}
		if wantsJSON(r) {
			jsonResponse(w, map[string]interface{}{"status": "ok", "message": "Authorization POC - Test Application"}, http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, buildPageData(r, false))
	})

	log.Printf("Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
