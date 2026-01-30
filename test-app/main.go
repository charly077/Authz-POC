package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

var startTime = time.Now()

func jsonResponse(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// wantsJSON returns true if the client prefers JSON (API calls, curl, etc.)
func wantsJSON(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/json") {
		return true
	}
	// If no Accept header or only */* (like curl), check for explicit format param
	if r.URL.Query().Get("format") == "json" {
		return true
	}
	return false
}

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

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	http.HandleFunc("/public", func(w http.ResponseWriter, r *http.Request) {
		if wantsJSON(r) {
			jsonResponse(w, map[string]interface{}{
				"status":  "ok",
				"message": "Public content - visible to everyone",
				"path":    r.URL.Path,
				"time":    time.Now().Format(time.RFC3339),
			}, http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, buildPageData(r, true))
	})

	// Logout: redirect to Keycloak end-session, which then redirects back to Envoy /signout to clear cookies
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
				"status":   "ok",
				"message":  "Protected content - access granted",
				"user":     user,
				"metadata": metadata,
				"path":     r.URL.Path,
				"method":   r.Method,
				"time":     time.Now().Format(time.RFC3339),
			}, http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, buildPageData(r, false))
	})

	http.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		if wantsJSON(r) {
			jsonResponse(w, map[string]interface{}{
				"status":  "healthy",
				"service": "test-app",
				"uptime":  time.Since(startTime).String(),
			}, http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		tmpl.Execute(w, buildPageData(r, false))
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			if wantsJSON(r) {
				jsonResponse(w, map[string]string{
					"status":  "error",
					"message": "Not found",
					"path":    r.URL.Path,
				}, http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, "Not found: %s", r.URL.Path)
			return
		}

		if wantsJSON(r) {
			jsonResponse(w, map[string]interface{}{
				"status":  "ok",
				"message": "Authorization POC - Test Application",
			}, http.StatusOK)
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
