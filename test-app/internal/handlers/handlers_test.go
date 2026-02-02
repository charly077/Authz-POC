package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"test-app/internal/config"
	"test-app/internal/store"
)

// setupFGA starts a mock OpenFGA server and configures the config package.
// Returns cleanup function.
func setupFGA(t *testing.T, handler http.HandlerFunc) func() {
	t.Helper()
	server := httptest.NewServer(handler)
	origURL := config.OpenfgaURL
	origReady := config.FgaReady
	origStore := config.FgaStoreId
	origModel := config.FgaModelId

	config.OpenfgaURL = server.URL
	config.FgaReady = true
	config.FgaStoreId = "test-store"
	config.FgaModelId = "test-model"

	return func() {
		server.Close()
		config.OpenfgaURL = origURL
		config.FgaReady = origReady
		config.FgaStoreId = origStore
		config.FgaModelId = origModel
	}
}

// resetStore resets the global store and returns a cleanup function.
func resetStore(t *testing.T) func() {
	t.Helper()
	origData := store.Data
	store.Data = &store.DataStore{
		Dossiers:             make(map[string]*store.Dossier),
		GuardianshipRequests: []store.GuardianshipRequest{},
		Guardianships:        make(map[string][]string),
	}
	return func() {
		store.Data = origData
	}
}

func TestDossiersList_FgaNotReady(t *testing.T) {
	origReady := config.FgaReady
	defer func() { config.FgaReady = origReady }()
	config.FgaReady = false

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/dossiers", nil)
	DossiersList(w, r)

	if w.Code != 503 {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestDossiersList_Empty(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()

	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "list-objects") {
			json.NewEncoder(w).Encode(map[string]interface{}{"objects": []string{}})
			return
		}
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/dossiers", nil)
	req.Header.Set("x-current-user", "alice")
	DossiersList(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	dossiers := body["dossiers"].([]interface{})
	if len(dossiers) != 0 {
		t.Errorf("dossiers count = %d, want 0", len(dossiers))
	}
}

func TestDossiersList_WithDossiers(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()

	store.Data.Dossiers["d1"] = &store.Dossier{Title: "Tax Return 2024", Content: "Annual tax filing", Type: "tax", Owner: "alice"}

	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "list-objects") {
			json.NewEncoder(w).Encode(map[string]interface{}{"objects": []interface{}{"dossier:d1"}})
			return
		}
		if strings.Contains(r.URL.Path, "check") {
			json.NewEncoder(w).Encode(map[string]interface{}{"allowed": true})
			return
		}
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/dossiers", nil)
	req.Header.Set("x-current-user", "alice")
	DossiersList(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	dossiers := body["dossiers"].([]interface{})
	if len(dossiers) != 1 {
		t.Errorf("dossiers count = %d, want 1", len(dossiers))
	}
	first := dossiers[0].(map[string]interface{})
	if first["title"] != "Tax Return 2024" {
		t.Errorf("title = %v, want Tax Return 2024", first["title"])
	}
	if first["canEdit"] != true {
		t.Errorf("canEdit = %v, want true", first["canEdit"])
	}
}

func TestDossiersCreate_MissingTitle(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers", strings.NewReader(`{"type":"tax"}`))
	req.Header.Set("x-current-user", "alice")
	DossiersCreate(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestDossiersCreate_InvalidType(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers", strings.NewReader(`{"title":"Test","type":"invalid"}`))
	req.Header.Set("x-current-user", "alice")
	DossiersCreate(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestDossiersCreate_Valid(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers", strings.NewReader(`{"title":"Tax Return 2024","content":"Annual filing","type":"tax"}`))
	req.Header.Set("x-current-user", "alice")
	DossiersCreate(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	if body["title"] != "Tax Return 2024" {
		t.Errorf("title = %v, want Tax Return 2024", body["title"])
	}
	if body["owner"] != "alice" {
		t.Errorf("owner = %v, want alice", body["owner"])
	}

	store.Mu.RLock()
	count := len(store.Data.Dossiers)
	store.Mu.RUnlock()
	if count != 1 {
		t.Errorf("store dossier count = %d, want 1", count)
	}
}

func TestGuardianshipsList_Empty(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/dossiers/guardianships", nil)
	req.Header.Set("x-current-user", "alice")
	GuardianshipsList(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	guardians := body["guardians"].([]interface{})
	if len(guardians) != 0 {
		t.Errorf("guardians count = %d, want 0", len(guardians))
	}
	wards := body["wards"].([]interface{})
	if len(wards) != 0 {
		t.Errorf("wards count = %d, want 0", len(wards))
	}
}

func TestGuardianshipsList_WithData(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()

	// bob is a guardian of alice
	store.Data.Guardianships["alice"] = []string{"bob"}
	store.Data.GuardianshipRequests = []store.GuardianshipRequest{
		{Id: "r1", From: "charlie", To: "alice", Status: "pending"},
		{Id: "r2", From: "alice", To: "dave", Status: "pending"},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/dossiers/guardianships", nil)
	req.Header.Set("x-current-user", "alice")
	GuardianshipsList(w, req)

	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)

	guardians := body["guardians"].([]interface{})
	if len(guardians) != 1 {
		t.Errorf("guardians = %d, want 1", len(guardians))
	}
	incoming := body["incoming"].([]interface{})
	if len(incoming) != 1 {
		t.Errorf("incoming = %d, want 1", len(incoming))
	}
	outgoing := body["outgoing"].([]interface{})
	if len(outgoing) != 1 {
		t.Errorf("outgoing = %d, want 1", len(outgoing))
	}
}

func TestGuardianshipRequest_ToSelf(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers/guardianships/request", strings.NewReader(`{"to":"alice"}`))
	req.Header.Set("x-current-user", "alice")
	GuardianshipRequest(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestGuardianshipRequest_Valid(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers/guardianships/request", strings.NewReader(`{"to":"bob"}`))
	req.Header.Set("x-current-user", "alice")
	GuardianshipRequest(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if len(store.Data.GuardianshipRequests) != 1 {
		t.Errorf("GuardianshipRequests count = %d, want 1", len(store.Data.GuardianshipRequests))
	}
}

func TestDebugTuples_FgaNotReady(t *testing.T) {
	origReady := config.FgaReady
	defer func() { config.FgaReady = origReady }()
	config.FgaReady = false

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/debug/tuples", nil)
	DebugTuples(w, req)

	if w.Code != 503 {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestDebugTuples_WithTuples(t *testing.T) {
	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "read") {
			resp := map[string]interface{}{
				"tuples": []interface{}{
					map[string]interface{}{
						"key": map[string]interface{}{
							"user":     "user:alice",
							"relation": "owner",
							"object":   "dossier:d1",
						},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		fmt.Fprintln(w, "{}")
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/debug/tuples", nil)
	DebugTuples(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	tuples := body["tuples"].([]interface{})
	if len(tuples) != 1 {
		t.Errorf("tuples count = %d, want 1", len(tuples))
	}
	first := tuples[0].(map[string]interface{})
	if first["user"] != "user:alice" {
		t.Errorf("user = %v, want user:alice", first["user"])
	}
}
