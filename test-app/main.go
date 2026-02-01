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

	http.HandleFunc("/animals", func(w http.ResponseWriter, r *http.Request) {
		user := httputil.GetUser(r)
		if user == "anonymous" {
			http.Redirect(w, r, "/home", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		templates.Animals.Execute(w, templates.AnimalsPageData{Username: user})
	})

	http.HandleFunc("/api/animals/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			handlers.AnimalsList(w, r)
		}
	})
	http.HandleFunc("/api/animals/create", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			handlers.AnimalsCreate(w, r)
		}
	})
	http.HandleFunc("/api/animals/friends", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			handlers.FriendsList(w, r)
		}
	})
	http.HandleFunc("/api/animals/friends/request", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			handlers.FriendsRequest(w, r)
		}
	})
	http.HandleFunc("/api/animals/debug/tuples", func(w http.ResponseWriter, r *http.Request) {
		handlers.DebugTuples(w, r)
	})

	http.HandleFunc("/api/animals/friends/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/animals/friends/")
		parts := strings.Split(path, "/")

		if len(parts) == 2 && parts[1] == "accept" && r.Method == "POST" {
			handlers.FriendsAccept(w, r, parts[0])
			return
		}
		if len(parts) == 2 && parts[1] == "deny" && r.Method == "POST" {
			handlers.FriendsDeny(w, r, parts[0])
			return
		}
		if len(parts) == 1 && r.Method == "DELETE" {
			handlers.FriendsRemove(w, r, parts[0])
			return
		}
		httputil.JSONError(w, "Not found", 404)
	})

	http.HandleFunc("/api/animals/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/animals/")
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
				handlers.AnimalsUpdate(w, r, id)
			case "DELETE":
				handlers.AnimalsDelete(w, r, id)
			default:
				httputil.JSONError(w, "Method not allowed", 405)
			}
			return
		}
		if len(parts) == 2 && parts[1] == "relations" {
			id := parts[0]
			switch r.Method {
			case "GET":
				handlers.AnimalsRelationsGet(w, r, id)
			case "POST":
				handlers.AnimalsRelationsAdd(w, r, id)
			case "DELETE":
				handlers.AnimalsRelationsDelete(w, r, id)
			default:
				httputil.JSONError(w, "Method not allowed", 405)
			}
			return
		}
		httputil.JSONError(w, "Not found", 404)
	})

	http.HandleFunc("/api/animals/status", func(w http.ResponseWriter, r *http.Request) {
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
