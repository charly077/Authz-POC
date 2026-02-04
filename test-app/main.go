package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"test-app/internal/config"
	"test-app/internal/fga"
	"test-app/internal/handlers"
	"test-app/internal/httputil"
	"test-app/internal/store"
	"test-app/internal/templates"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	config.ExternalURL = os.Getenv("EXTERNAL_URL")
	if config.ExternalURL == "" {
		config.ExternalURL = "http://localhost:8000"
	}
	config.OpenfgaURL = os.Getenv("OPENFGA_URL")
	if config.OpenfgaURL == "" {
		config.OpenfgaURL = "http://openfga:8080"
	}
	config.AuditURL = os.Getenv("AUDIT_URL")
	if config.AuditURL == "" {
		config.AuditURL = "http://ai-manager:5000"
	}

	templates.Init("internal/templates")
	store.Load()

	go func() {
		fga.LoadConfig()
		store.RehydrateTuples(fga.Write)
	}()

	http.HandleFunc("/public", func(w http.ResponseWriter, r *http.Request) {
		if httputil.WantsJSON(r) {
			httputil.JSONResponse(w, map[string]interface{}{
				"status": "ok", "message": "Public content - visible to everyone",
				"path": r.URL.Path, "time": time.Now().Format(time.RFC3339),
			}, http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		templates.Page.Execute(w, templates.BuildPageData(r, true))
	})

	http.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		keycloakLogout := config.ExternalURL + "/login/realms/AuthorizationRealm/protocol/openid-connect/logout" +
			"?client_id=envoy" +
			"&post_logout_redirect_uri=" + url.QueryEscape(config.ExternalURL+"/signout")
		if idToken, err := r.Cookie("IdToken"); err == nil && idToken.Value != "" {
			keycloakLogout += "&id_token_hint=" + url.QueryEscape(idToken.Value)
		}
		http.Redirect(w, r, keycloakLogout, http.StatusFound)
	})

	http.HandleFunc("/api/protected", func(w http.ResponseWriter, r *http.Request) {
		user := r.Header.Get("x-current-user")
		metadata := r.Header.Get("x-user-metadata")
		if httputil.WantsJSON(r) {
			httputil.JSONResponse(w, map[string]interface{}{
				"status": "ok", "message": "Protected content - access granted",
				"user": user, "metadata": metadata,
				"path": r.URL.Path, "method": r.Method, "time": time.Now().Format(time.RFC3339),
			}, http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		templates.Page.Execute(w, templates.BuildPageData(r, false))
	})

	http.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		if httputil.WantsJSON(r) {
			httputil.JSONResponse(w, map[string]interface{}{
				"status": "healthy", "service": "test-app",
				"uptime": time.Since(config.StartTime).String(), "fgaReady": config.FgaReady,
			}, http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		templates.Page.Execute(w, templates.BuildPageData(r, false))
	})

	http.HandleFunc("/dossiers", func(w http.ResponseWriter, r *http.Request) {
		user := httputil.GetUser(r)
		if user == "anonymous" {
			http.Redirect(w, r, "/home", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		templates.Dossiers.Execute(w, templates.DossiersPageData{Username: user})
	})

	http.HandleFunc("/api/dossiers/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			handlers.DossiersList(w, r)
		}
	})
	http.HandleFunc("/api/dossiers/create", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			handlers.DossiersCreate(w, r)
		}
	})
	http.HandleFunc("/api/dossiers/guardianships", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			handlers.GuardianshipsList(w, r)
		}
	})
	http.HandleFunc("/api/dossiers/guardianships/request", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			handlers.GuardianshipRequest(w, r)
		}
	})
	http.HandleFunc("/api/dossiers/organizations", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			handlers.OrganizationsList(w, r)
		case "POST":
			handlers.OrganizationsCreate(w, r)
		default:
			httputil.JSONError(w, "Method not allowed", 405)
		}
	})
	http.HandleFunc("/api/dossiers/organizations/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/dossiers/organizations/")
		parts := strings.Split(path, "/")
		if len(parts) == 2 && parts[1] == "members" {
			switch r.Method {
			case "POST":
				handlers.OrganizationsAddMember(w, r, parts[0])
			case "DELETE":
				handlers.OrganizationsRemoveMember(w, r, parts[0])
			default:
				httputil.JSONError(w, "Method not allowed", 405)
			}
			return
		}
		if len(parts) == 2 && parts[1] == "admins" {
			switch r.Method {
			case "POST":
				handlers.OrganizationsAddAdmin(w, r, parts[0])
			case "DELETE":
				handlers.OrganizationsRemoveAdmin(w, r, parts[0])
			default:
				httputil.JSONError(w, "Method not allowed", 405)
			}
			return
		}
		// DELETE /api/dossiers/organizations/{id} - delete organization
		if len(parts) == 1 && parts[0] != "" && r.Method == "DELETE" {
			handlers.OrganizationsDelete(w, r, parts[0])
			return
		}
		httputil.JSONError(w, "Not found", 404)
	})
	http.HandleFunc("/api/dossiers/debug/tuples", func(w http.ResponseWriter, r *http.Request) {
		handlers.DebugTuples(w, r)
	})

	http.HandleFunc("/api/dossiers/guardianships/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/dossiers/guardianships/")
		parts := strings.Split(path, "/")

		if len(parts) == 2 && parts[1] == "accept" && r.Method == "POST" {
			handlers.GuardianshipAccept(w, r, parts[0])
			return
		}
		if len(parts) == 2 && parts[1] == "deny" && r.Method == "POST" {
			handlers.GuardianshipDeny(w, r, parts[0])
			return
		}
		if len(parts) == 1 && r.Method == "DELETE" {
			handlers.GuardianshipRemove(w, r, parts[0])
			return
		}
		httputil.JSONError(w, "Not found", 404)
	})

	http.HandleFunc("/api/dossiers/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/dossiers/")
		if strings.HasPrefix(path, "list") || strings.HasPrefix(path, "create") ||
			strings.HasPrefix(path, "guardianships") || strings.HasPrefix(path, "debug") ||
			strings.HasPrefix(path, "status") || strings.HasPrefix(path, "organizations") {
			return
		}

		parts := strings.Split(path, "/")
		if len(parts) == 1 && parts[0] != "" {
			id := parts[0]
			switch r.Method {
			case "PUT":
				handlers.DossiersUpdate(w, r, id)
			case "DELETE":
				handlers.DossiersDelete(w, r, id)
			default:
				httputil.JSONError(w, "Method not allowed", 405)
			}
			return
		}
		if len(parts) == 2 && parts[1] == "relations" {
			id := parts[0]
			switch r.Method {
			case "GET":
				handlers.DossiersRelationsGet(w, r, id)
			case "POST":
				handlers.DossiersRelationsAdd(w, r, id)
			case "DELETE":
				handlers.DossiersRelationsDelete(w, r, id)
			default:
				httputil.JSONError(w, "Method not allowed", 405)
			}
			return
		}
		if len(parts) == 2 && parts[1] == "toggle-public" && r.Method == "POST" {
			handlers.DossiersTogglePublic(w, r, parts[0])
			return
		}
		if len(parts) == 2 && parts[1] == "block" && r.Method == "POST" {
			handlers.DossiersBlock(w, r, parts[0])
			return
		}
		if len(parts) == 2 && parts[1] == "unblock" && r.Method == "POST" {
			handlers.DossiersUnblock(w, r, parts[0])
			return
		}
		if len(parts) == 2 && parts[1] == "emergency-check" && r.Method == "POST" {
			handlers.DossiersEmergencyCheck(w, r, parts[0])
			return
		}
		httputil.JSONError(w, "Not found", 404)
	})

	http.HandleFunc("/api/dossiers/status", func(w http.ResponseWriter, r *http.Request) {
		httputil.JSONResponse(w, map[string]interface{}{"ready": config.FgaReady, "storeId": config.FgaStoreId, "modelId": config.FgaModelId}, 200)
	})

	http.HandleFunc("/home", func(w http.ResponseWriter, r *http.Request) {
		if httputil.WantsJSON(r) {
			httputil.JSONResponse(w, map[string]interface{}{"status": "ok", "message": "Authorization POC - Test Application"}, http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		templates.Page.Execute(w, templates.BuildPageData(r, false))
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/public", http.StatusFound)
			return
		}
		if httputil.WantsJSON(r) {
			httputil.JSONResponse(w, map[string]string{"status": "error", "message": "Not found", "path": r.URL.Path}, http.StatusNotFound)
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
