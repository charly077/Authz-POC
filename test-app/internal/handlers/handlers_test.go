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
		Organizations:        make(map[string]*store.Organization),
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

// Scenario A: Organization-Based Access

func TestOrganizationsCreate(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers/organizations", strings.NewReader(`{"name":"BOSA","members":["alice","bob"]}`))
	req.Header.Set("x-current-user", "admin")
	OrganizationsCreate(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	if body["name"] != "BOSA" {
		t.Errorf("name = %v, want BOSA", body["name"])
	}
	store.Mu.RLock()
	count := len(store.Data.Organizations)
	store.Mu.RUnlock()
	if count != 1 {
		t.Errorf("org count = %d, want 1", count)
	}
}

func TestOrganizationsCreate_CreatorBecomesAdmin(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers/organizations", strings.NewReader(`{"name":"BOSA","members":["alice"]}`))
	req.Header.Set("x-current-user", "alice")
	OrganizationsCreate(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)

	admins, ok := body["admins"].([]interface{})
	if !ok || len(admins) != 1 {
		t.Fatalf("admins = %v, want [alice]", body["admins"])
	}
	if admins[0] != "alice" {
		t.Errorf("admins[0] = %v, want alice", admins[0])
	}

	members := body["members"].([]interface{})
	found := false
	for _, m := range members {
		if m == "alice" {
			found = true
		}
	}
	if !found {
		t.Errorf("creator not in members: %v", members)
	}
}

func TestOrganizationsCreate_MissingName(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers/organizations", strings.NewReader(`{}`))
	req.Header.Set("x-current-user", "admin")
	OrganizationsCreate(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// fgaCheckMock returns an FGA handler that allows can_manage checks for the given admin user.
func fgaCheckMock(adminUser string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "check") {
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			tupleKey, _ := body["tuple_key"].(map[string]interface{})
			user, _ := tupleKey["user"].(string)
			if user == "user:"+adminUser {
				json.NewEncoder(w).Encode(map[string]interface{}{"allowed": true})
				return
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"allowed": false})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}
}

func TestOrganizationsAddMember_AsAdmin(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	store.Data.Organizations["org1"] = &store.Organization{Name: "BOSA", Members: []string{"alice"}, Admins: []string{"alice"}}

	cleanFGA := setupFGA(t, fgaCheckMock("alice"))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers/organizations/org1/members", strings.NewReader(`{"member":"bob"}`))
	req.Header.Set("x-current-user", "alice")
	OrganizationsAddMember(w, req, "org1")

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	store.Mu.RLock()
	members := store.Data.Organizations["org1"].Members
	store.Mu.RUnlock()
	if len(members) != 2 {
		t.Errorf("members = %d, want 2", len(members))
	}
}

func TestOrganizationsAddMember_Unauthorized(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	store.Data.Organizations["org1"] = &store.Organization{Name: "BOSA", Members: []string{"alice", "bob"}, Admins: []string{"alice"}}

	cleanFGA := setupFGA(t, fgaCheckMock("alice"))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers/organizations/org1/members", strings.NewReader(`{"member":"charlie"}`))
	req.Header.Set("x-current-user", "bob")
	OrganizationsAddMember(w, req, "org1")

	if w.Code != 403 {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestOrganizationsAddMember_NotFound(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	cleanFGA := setupFGA(t, fgaCheckMock("alice"))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers/organizations/missing/members", strings.NewReader(`{"member":"bob"}`))
	req.Header.Set("x-current-user", "alice")
	OrganizationsAddMember(w, req, "missing")

	// alice passes the can_manage check (mock allows it) but org is not found
	// Actually the check will fail because the mock only checks user, not object existence
	// The FGA check returns true for alice regardless, so we get 404
	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestOrganizationsRemoveMember_Unauthorized(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	store.Data.Organizations["org1"] = &store.Organization{Name: "BOSA", Members: []string{"alice", "bob", "charlie"}, Admins: []string{"alice"}}

	cleanFGA := setupFGA(t, fgaCheckMock("alice"))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/dossiers/organizations/org1/members", strings.NewReader(`{"member":"charlie"}`))
	req.Header.Set("x-current-user", "bob")
	OrganizationsRemoveMember(w, req, "org1")

	if w.Code != 403 {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestOrganizationsAddAdmin(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	store.Data.Organizations["org1"] = &store.Organization{Name: "BOSA", Members: []string{"alice", "bob"}, Admins: []string{"alice"}}

	cleanFGA := setupFGA(t, fgaCheckMock("alice"))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers/organizations/org1/admins", strings.NewReader(`{"user":"bob"}`))
	req.Header.Set("x-current-user", "alice")
	OrganizationsAddAdmin(w, req, "org1")

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	store.Mu.RLock()
	admins := store.Data.Organizations["org1"].Admins
	store.Mu.RUnlock()
	if len(admins) != 2 {
		t.Errorf("admins = %d, want 2", len(admins))
	}
}

func TestOrganizationsRemoveAdmin(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	store.Data.Organizations["org1"] = &store.Organization{Name: "BOSA", Members: []string{"alice", "bob"}, Admins: []string{"alice", "bob"}}

	cleanFGA := setupFGA(t, fgaCheckMock("alice"))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/dossiers/organizations/org1/admins", strings.NewReader(`{"user":"bob"}`))
	req.Header.Set("x-current-user", "alice")
	OrganizationsRemoveAdmin(w, req, "org1")

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	store.Mu.RLock()
	admins := store.Data.Organizations["org1"].Admins
	store.Mu.RUnlock()
	if len(admins) != 1 {
		t.Errorf("admins = %d, want 1", len(admins))
	}
	if admins[0] != "alice" {
		t.Errorf("remaining admin = %v, want alice", admins[0])
	}
}

func TestDossierOrgAccess(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	store.Data.Organizations["org1"] = &store.Organization{Name: "BOSA", Members: []string{"alice"}, Admins: []string{"alice"}}
	store.Data.Dossiers["d1"] = &store.Dossier{Title: "Org Dossier", Type: "general", Owner: "admin", OrgId: "org1"}

	// FGA mock: alice can view (org member), bob cannot
	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(r.URL.Path, "list-objects") {
			user, _ := body["user"].(string)
			if user == "user:alice" {
				json.NewEncoder(w).Encode(map[string]interface{}{"objects": []interface{}{"dossier:d1"}})
			} else {
				json.NewEncoder(w).Encode(map[string]interface{}{"objects": []interface{}{}})
			}
			return
		}
		if strings.Contains(r.URL.Path, "check") {
			json.NewEncoder(w).Encode(map[string]interface{}{"allowed": true})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	// alice sees the dossier
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/dossiers", nil)
	req.Header.Set("x-current-user", "alice")
	DossiersList(w, req)

	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	dossiers := body["dossiers"].([]interface{})
	if len(dossiers) != 1 {
		t.Errorf("alice dossiers = %d, want 1", len(dossiers))
	}

	// bob sees nothing
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/api/dossiers", nil)
	req2.Header.Set("x-current-user", "bob")
	DossiersList(w2, req2)

	var body2 map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&body2)
	dossiers2 := body2["dossiers"].([]interface{})
	if len(dossiers2) != 0 {
		t.Errorf("bob dossiers = %d, want 0", len(dossiers2))
	}
}

// Scenario B: Blocked Users

func TestDossierBlockedUser(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	store.Data.Dossiers["d1"] = &store.Dossier{Title: "Test", Type: "tax", Owner: "alice"}

	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers/d1/block", strings.NewReader(`{"targetUser":"bob"}`))
	req.Header.Set("x-current-user", "alice")
	DossiersBlock(w, req, "d1")

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	store.Mu.RLock()
	blocked := store.Data.Dossiers["d1"].BlockedUsers
	store.Mu.RUnlock()
	if len(blocked) != 1 || blocked[0] != "bob" {
		t.Errorf("blocked = %v, want [bob]", blocked)
	}
}

func TestDossierBlockedUser_NotOwner(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	store.Data.Dossiers["d1"] = &store.Dossier{Title: "Test", Type: "tax", Owner: "alice"}

	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers/d1/block", strings.NewReader(`{"targetUser":"charlie"}`))
	req.Header.Set("x-current-user", "bob")
	DossiersBlock(w, req, "d1")

	if w.Code != 403 {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestDossierUnblock(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	store.Data.Dossiers["d1"] = &store.Dossier{Title: "Test", Type: "tax", Owner: "alice", BlockedUsers: []string{"bob"}}

	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers/d1/unblock", strings.NewReader(`{"targetUser":"bob"}`))
	req.Header.Set("x-current-user", "alice")
	DossiersUnblock(w, req, "d1")

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	store.Mu.RLock()
	blocked := store.Data.Dossiers["d1"].BlockedUsers
	store.Mu.RUnlock()
	if len(blocked) != 0 {
		t.Errorf("blocked = %v, want []", blocked)
	}
}

// Scenario C: Public Dossiers

func TestDossierTogglePublic(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	store.Data.Dossiers["d1"] = &store.Dossier{Title: "Test", Type: "tax", Owner: "alice"}

	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	// Toggle ON
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers/d1/toggle-public", nil)
	req.Header.Set("x-current-user", "alice")
	DossiersTogglePublic(w, req, "d1")

	if w.Code != 200 {
		t.Errorf("toggle on status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	if body["isPublic"] != true {
		t.Errorf("isPublic = %v, want true", body["isPublic"])
	}

	// Toggle OFF
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/dossiers/d1/toggle-public", nil)
	req2.Header.Set("x-current-user", "alice")
	DossiersTogglePublic(w2, req2, "d1")

	var body2 map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&body2)
	if body2["isPublic"] != false {
		t.Errorf("isPublic = %v, want false", body2["isPublic"])
	}
}

func TestDossierTogglePublic_NotOwner(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	store.Data.Dossiers["d1"] = &store.Dossier{Title: "Test", Type: "tax", Owner: "alice"}

	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers/d1/toggle-public", nil)
	req.Header.Set("x-current-user", "bob")
	DossiersTogglePublic(w, req, "d1")

	if w.Code != 403 {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestPublicDossierVisibleToAll(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	store.Data.Dossiers["d1"] = &store.Dossier{Title: "Public Doc", Type: "general", Owner: "alice", Public: true}

	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "list-objects") {
			json.NewEncoder(w).Encode(map[string]interface{}{"objects": []interface{}{"dossier:d1"}})
			return
		}
		if strings.Contains(r.URL.Path, "check") {
			json.NewEncoder(w).Encode(map[string]interface{}{"allowed": false})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	// random user can see the public dossier (via list-objects returning it)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/dossiers", nil)
	req.Header.Set("x-current-user", "random-user")
	DossiersList(w, req)

	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	dossiers := body["dossiers"].([]interface{})
	if len(dossiers) != 1 {
		t.Errorf("dossiers = %d, want 1", len(dossiers))
	}
	first := dossiers[0].(map[string]interface{})
	if first["isPublic"] != true {
		t.Errorf("isPublic = %v, want true", first["isPublic"])
	}
}

// Scenario D: Contextual Tuples (Emergency Access)

func TestEmergencyCheck(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	store.Data.Dossiers["d1"] = &store.Dossier{Title: "Test", Type: "tax", Owner: "alice"}

	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "check") {
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			// If contextual tuples present, allow
			if ct, ok := body["contextual_tuples"]; ok {
				ctMap, _ := ct.(map[string]interface{})
				keys, _ := ctMap["tuple_keys"].([]interface{})
				if len(keys) > 0 {
					json.NewEncoder(w).Encode(map[string]interface{}{"allowed": true})
					return
				}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"allowed": false})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers/d1/emergency-check", strings.NewReader(`{"user":"bob","relation":"viewer"}`))
	req.Header.Set("x-current-user", "admin")
	DossiersEmergencyCheck(w, req, "d1")

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	if body["allowed"] != true {
		t.Errorf("allowed = %v, want true", body["allowed"])
	}
	if body["contextual"] != true {
		t.Errorf("contextual = %v, want true", body["contextual"])
	}
}

func TestEmergencyCheck_NotFound(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers/missing/emergency-check", strings.NewReader(`{"user":"bob"}`))
	DossiersEmergencyCheck(w, req, "missing")

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestDossiersCreate_WithOrgAndPublic(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	store.Data.Organizations["org1"] = &store.Organization{Name: "BOSA", Members: []string{"alice"}, Admins: []string{"alice"}}

	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers", strings.NewReader(`{"title":"Org Doc","type":"general","orgId":"org1","public":true}`))
	req.Header.Set("x-current-user", "alice")
	DossiersCreate(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	if body["orgId"] != "org1" {
		t.Errorf("orgId = %v, want org1", body["orgId"])
	}
	if body["isPublic"] != true {
		t.Errorf("isPublic = %v, want true", body["isPublic"])
	}
}

func TestDossiersCreate_OrgNotFound(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	cleanFGA := setupFGA(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer cleanFGA()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/dossiers", strings.NewReader(`{"title":"Test","type":"general","orgId":"missing"}`))
	req.Header.Set("x-current-user", "alice")
	DossiersCreate(w, req)

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestOrganizationsList(t *testing.T) {
	cleanStore := resetStore(t)
	defer cleanStore()
	store.Data.Organizations["org1"] = &store.Organization{Name: "BOSA", Members: []string{"alice"}, Admins: []string{"alice"}}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/dossiers/organizations", nil)
	OrganizationsList(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	orgs := body["organizations"].([]interface{})
	if len(orgs) != 1 {
		t.Errorf("orgs = %d, want 1", len(orgs))
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
