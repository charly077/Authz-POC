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
		Animals:        make(map[string]*store.Animal),
		FriendRequests: []store.FriendRequest{},
		Friends:        make(map[string][]string),
	}
	return func() {
		store.Data = origData
	}
}

func TestAnimalsList_FgaNotReady(t *testing.T) {
	origReady := config.FgaReady
	defer func() { config.FgaReady = origReady }()
	config.FgaReady = false

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/animals", nil)
	AnimalsList(w, r)

	if w.Code != 503 {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

func TestAnimalsList_Empty(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()

	// Mock FGA: list-objects returns empty, check not called
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
	req := httptest.NewRequest("GET", "/api/animals", nil)
	req.Header.Set("x-current-user", "alice")
	AnimalsList(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	animals := body["animals"].([]interface{})
	if len(animals) != 0 {
		t.Errorf("animals count = %d, want 0", len(animals))
	}
}

func TestAnimalsList_WithAnimals(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()

	store.Data.Animals["a1"] = &store.Animal{Name: "Rex", Species: "Dog", Age: 3, Owner: "alice"}

	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "list-objects") {
			json.NewEncoder(w).Encode(map[string]interface{}{"objects": []interface{}{"animal:a1"}})
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
	req := httptest.NewRequest("GET", "/api/animals", nil)
	req.Header.Set("x-current-user", "alice")
	AnimalsList(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	animals := body["animals"].([]interface{})
	if len(animals) != 1 {
		t.Errorf("animals count = %d, want 1", len(animals))
	}
	first := animals[0].(map[string]interface{})
	if first["name"] != "Rex" {
		t.Errorf("name = %v, want Rex", first["name"])
	}
	if first["canEdit"] != true {
		t.Errorf("canEdit = %v, want true", first["canEdit"])
	}
}

func TestAnimalsCreate_MissingName(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/animals", strings.NewReader(`{"species":"Dog"}`))
	req.Header.Set("x-current-user", "alice")
	AnimalsCreate(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAnimalsCreate_Valid(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/animals", strings.NewReader(`{"name":"Rex","species":"Dog","age":3}`))
	req.Header.Set("x-current-user", "alice")
	AnimalsCreate(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	if body["name"] != "Rex" {
		t.Errorf("name = %v, want Rex", body["name"])
	}
	if body["owner"] != "alice" {
		t.Errorf("owner = %v, want alice", body["owner"])
	}

	store.Mu.RLock()
	count := len(store.Data.Animals)
	store.Mu.RUnlock()
	if count != 1 {
		t.Errorf("store animal count = %d, want 1", count)
	}
}

func TestFriendsList_Empty(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/friends", nil)
	req.Header.Set("x-current-user", "alice")
	FriendsList(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	friends := body["friends"].([]interface{})
	if len(friends) != 0 {
		t.Errorf("friends count = %d, want 0", len(friends))
	}
}

func TestFriendsList_WithData(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()

	store.Data.Friends["alice"] = []string{"bob"}
	store.Data.FriendRequests = []store.FriendRequest{
		{Id: "r1", From: "charlie", To: "alice", Status: "pending"},
		{Id: "r2", From: "alice", To: "dave", Status: "pending"},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/friends", nil)
	req.Header.Set("x-current-user", "alice")
	FriendsList(w, req)

	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)

	friends := body["friends"].([]interface{})
	if len(friends) != 1 {
		t.Errorf("friends = %d, want 1", len(friends))
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

func TestFriendsRequest_ToSelf(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/friends/request", strings.NewReader(`{"to":"alice"}`))
	req.Header.Set("x-current-user", "alice")
	FriendsRequest(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestFriendsRequest_Valid(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/friends/request", strings.NewReader(`{"to":"bob"}`))
	req.Header.Set("x-current-user", "alice")
	FriendsRequest(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if len(store.Data.FriendRequests) != 1 {
		t.Errorf("FriendRequests count = %d, want 1", len(store.Data.FriendRequests))
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
							"object":   "animal:a1",
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
