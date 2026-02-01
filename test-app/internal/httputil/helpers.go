package httputil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func JSONResponse(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func JSONError(w http.ResponseWriter, msg string, status int) {
	JSONResponse(w, map[string]string{"error": msg}, status)
}

func WantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json") ||
		r.URL.Query().Get("format") == "json"
}

func GetUser(r *http.Request) string {
	user := r.Header.Get("x-current-user")
	if user == "" {
		user = "anonymous"
	}
	return user
}

func ReadBody(r *http.Request) (map[string]interface{}, error) {
	var m map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("failed to decode request body: %w", err)
	}
	return m, nil
}

func GetString(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func GetInt(m map[string]interface{}, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func Contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
