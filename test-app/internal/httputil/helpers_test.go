package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONResponse(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"key": "value"}
	JSONResponse(w, data, http.StatusCreated)

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	var got map[string]string
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["key"] != "value" {
		t.Errorf("body key = %q, want %q", got["key"], "value")
	}
}

func TestJSONError(t *testing.T) {
	w := httptest.NewRecorder()
	JSONError(w, "bad request", http.StatusBadRequest)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var got map[string]string
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["error"] != "bad request" {
		t.Errorf("error = %q, want %q", got["error"], "bad request")
	}
}

func TestWantsJSON(t *testing.T) {
	tests := []struct {
		name   string
		accept string
		query  string
		want   bool
	}{
		{"accept header", "application/json", "", true},
		{"format query", "", "json", true},
		{"neither", "text/html", "", false},
		{"both", "application/json", "json", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/test", nil)
			if tt.accept != "" {
				r.Header.Set("Accept", tt.accept)
			}
			if tt.query != "" {
				q := r.URL.Query()
				q.Set("format", tt.query)
				r.URL.RawQuery = q.Encode()
			}
			if got := WantsJSON(r); got != tt.want {
				t.Errorf("WantsJSON() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetUser(t *testing.T) {
	t.Run("header present", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("x-current-user", "alice")
		if got := GetUser(r); got != "alice" {
			t.Errorf("GetUser() = %q, want %q", got, "alice")
		}
	})
	t.Run("header absent", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		if got := GetUser(r); got != "anonymous" {
			t.Errorf("GetUser() = %q, want %q", got, "anonymous")
		}
	})
}

func TestReadBody(t *testing.T) {
	t.Run("valid json", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"dog"}`))
		m, err := ReadBody(r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m["name"] != "dog" {
			t.Errorf("name = %v, want %q", m["name"], "dog")
		}
	})
	t.Run("invalid json", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/", strings.NewReader(`{invalid`))
		_, err := ReadBody(r)
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})
}

func TestGetString(t *testing.T) {
	m := map[string]interface{}{"name": "cat", "age": 5}
	if got := GetString(m, "name"); got != "cat" {
		t.Errorf("GetString(name) = %q, want %q", got, "cat")
	}
	if got := GetString(m, "missing"); got != "" {
		t.Errorf("GetString(missing) = %q, want empty", got)
	}
	if got := GetString(m, "age"); got != "" {
		t.Errorf("GetString(age) = %q, want empty (wrong type)", got)
	}
}

func TestGetInt(t *testing.T) {
	m := map[string]interface{}{"float": float64(42), "int": 7, "str": "nope"}
	if got := GetInt(m, "float"); got != 42 {
		t.Errorf("GetInt(float) = %d, want 42", got)
	}
	if got := GetInt(m, "int"); got != 7 {
		t.Errorf("GetInt(int) = %d, want 7", got)
	}
	if got := GetInt(m, "missing"); got != 0 {
		t.Errorf("GetInt(missing) = %d, want 0", got)
	}
	if got := GetInt(m, "str"); got != 0 {
		t.Errorf("GetInt(str) = %d, want 0", got)
	}
}

func TestContains(t *testing.T) {
	slice := []string{"a", "b", "c"}
	if !Contains(slice, "b") {
		t.Error("Contains(b) = false, want true")
	}
	if Contains(slice, "d") {
		t.Error("Contains(d) = true, want false")
	}
	if Contains(nil, "a") {
		t.Error("Contains(nil, a) = true, want false")
	}
}
