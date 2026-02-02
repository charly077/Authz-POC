package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestRandId(t *testing.T) {
	id := RandId()
	if len(id) != 8 {
		t.Errorf("RandId() length = %d, want 8", len(id))
	}
	if !regexp.MustCompile(`^[a-z0-9]{8}$`).MatchString(id) {
		t.Errorf("RandId() = %q, contains invalid chars", id)
	}
	id2 := RandId()
	if id == id2 {
		t.Errorf("two RandId() calls returned same value: %q", id)
	}
}

func TestRehydrateTuples_Empty(t *testing.T) {
	origData := Data
	defer func() { Data = origData }()

	Data = &DataStore{
		Dossiers:             make(map[string]*Dossier),
		GuardianshipRequests: []GuardianshipRequest{},
		Guardianships:        make(map[string][]string),
	}

	called := false
	fgaWrite := func(writes []TupleKey, deletes []TupleKey) error {
		called = true
		return nil
	}
	RehydrateTuples(fgaWrite)
	if called {
		t.Error("fgaWrite should not be called with empty data")
	}
}

func TestRehydrateTuples_WithData(t *testing.T) {
	origData := Data
	defer func() { Data = origData }()

	Data = &DataStore{
		Dossiers: map[string]*Dossier{
			"d1": {Title: "Tax Return 2024", Owner: "alice", Relations: []Relation{
				{User: "bob", Relation: "mandate_holder"},
			}},
		},
		GuardianshipRequests: []GuardianshipRequest{},
		Guardianships: map[string][]string{
			"alice": {"bob"},
		},
	}

	var allWrites []TupleKey
	fgaWrite := func(writes []TupleKey, deletes []TupleKey) error {
		allWrites = append(allWrites, writes...)
		return nil
	}
	RehydrateTuples(fgaWrite)

	// Expect: owner tuple, mandate_holder relation, guardian tuple = 3
	if len(allWrites) != 3 {
		t.Errorf("total writes = %d, want 3", len(allWrites))
	}
}

func TestRehydrateTuples_BatchSplitting(t *testing.T) {
	origData := Data
	defer func() { Data = origData }()

	dossiers := make(map[string]*Dossier)
	for i := 0; i < 12; i++ {
		id := RandId()
		dossiers[id] = &Dossier{Title: "dossier", Owner: "alice"}
	}
	Data = &DataStore{
		Dossiers:             dossiers,
		GuardianshipRequests: []GuardianshipRequest{},
		Guardianships:        make(map[string][]string),
	}

	batchCount := 0
	fgaWrite := func(writes []TupleKey, deletes []TupleKey) error {
		batchCount++
		if len(writes) > 10 {
			t.Errorf("batch size = %d, want <= 10", len(writes))
		}
		return nil
	}
	RehydrateTuples(fgaWrite)

	if batchCount != 2 {
		t.Errorf("batch count = %d, want 2", batchCount)
	}
}

func TestLoadSave_Roundtrip(t *testing.T) {
	origData := Data
	origFile := dataFile
	defer func() {
		Data = origData
		dataFile = origFile
	}()

	tmpDir := t.TempDir()
	dataFile = filepath.Join(tmpDir, "data", "dossiers.json")

	Data = &DataStore{
		Dossiers: map[string]*Dossier{
			"x1": {Title: "Health Record", Content: "Annual checkup", Type: "health", Owner: "alice"},
		},
		GuardianshipRequests: []GuardianshipRequest{{Id: "r1", From: "alice", To: "bob", Status: "pending"}},
		Guardianships:        map[string][]string{"alice": {"bob"}},
	}

	Save()

	raw, err := os.ReadFile(dataFile)
	if err != nil {
		t.Fatalf("saved file not found: %v", err)
	}

	Data = &DataStore{
		Dossiers:             make(map[string]*Dossier),
		GuardianshipRequests: []GuardianshipRequest{},
		Guardianships:        make(map[string][]string),
	}
	Load()

	if len(Data.Dossiers) != 1 {
		t.Fatalf("Dossiers count = %d, want 1", len(Data.Dossiers))
	}
	if Data.Dossiers["x1"].Title != "Health Record" {
		t.Errorf("Dossier title = %q, want %q", Data.Dossiers["x1"].Title, "Health Record")
	}
	if len(Data.GuardianshipRequests) != 1 {
		t.Errorf("GuardianshipRequests count = %d, want 1", len(Data.GuardianshipRequests))
	}
	if len(Data.Guardianships["alice"]) != 1 {
		t.Errorf("Guardianships[alice] count = %d, want 1", len(Data.Guardianships["alice"]))
	}

	// Verify JSON is valid
	var check DataStore
	if err := json.Unmarshal(raw, &check); err != nil {
		t.Errorf("saved JSON invalid: %v", err)
	}
}

func TestRehydrateTuples_WithOrg(t *testing.T) {
	origData := Data
	defer func() { Data = origData }()

	Data = &DataStore{
		Dossiers: map[string]*Dossier{
			"d1": {Title: "Org Doc", Owner: "alice", OrgId: "org1"},
		},
		GuardianshipRequests: []GuardianshipRequest{},
		Guardianships:        make(map[string][]string),
		Organizations: map[string]*Organization{
			"org1": {Name: "BOSA", Members: []string{"bob", "charlie"}},
		},
	}

	var allWrites []TupleKey
	fgaWrite := func(writes []TupleKey, deletes []TupleKey) error {
		allWrites = append(allWrites, writes...)
		return nil
	}
	RehydrateTuples(fgaWrite)

	// Expect: owner (1) + org_parent (1) + 2 org members = 4
	if len(allWrites) != 4 {
		t.Errorf("total writes = %d, want 4; writes: %+v", len(allWrites), allWrites)
	}

	// Verify org_parent tuple exists
	found := false
	for _, w := range allWrites {
		if w.Relation == "org_parent" && w.User == "organization:org1" {
			found = true
		}
	}
	if !found {
		t.Error("org_parent tuple not found in writes")
	}
}

func TestRehydrateTuples_WithPublic(t *testing.T) {
	origData := Data
	defer func() { Data = origData }()

	Data = &DataStore{
		Dossiers: map[string]*Dossier{
			"d1": {Title: "Public Doc", Owner: "alice", Public: true},
		},
		GuardianshipRequests: []GuardianshipRequest{},
		Guardianships:        make(map[string][]string),
		Organizations:        make(map[string]*Organization),
	}

	var allWrites []TupleKey
	fgaWrite := func(writes []TupleKey, deletes []TupleKey) error {
		allWrites = append(allWrites, writes...)
		return nil
	}
	RehydrateTuples(fgaWrite)

	// Expect: owner (1) + public wildcard (1) = 2
	if len(allWrites) != 2 {
		t.Errorf("total writes = %d, want 2; writes: %+v", len(allWrites), allWrites)
	}

	found := false
	for _, w := range allWrites {
		if w.Relation == "public" && w.User == "user:*" {
			found = true
		}
	}
	if !found {
		t.Error("public wildcard tuple not found in writes")
	}
}

func TestRehydrateTuples_WithBlocked(t *testing.T) {
	origData := Data
	defer func() { Data = origData }()

	Data = &DataStore{
		Dossiers: map[string]*Dossier{
			"d1": {Title: "Blocked Doc", Owner: "alice", BlockedUsers: []string{"bob", "charlie"}},
		},
		GuardianshipRequests: []GuardianshipRequest{},
		Guardianships:        make(map[string][]string),
		Organizations:        make(map[string]*Organization),
	}

	var allWrites []TupleKey
	fgaWrite := func(writes []TupleKey, deletes []TupleKey) error {
		allWrites = append(allWrites, writes...)
		return nil
	}
	RehydrateTuples(fgaWrite)

	// Expect: owner (1) + blocked (2) = 3
	if len(allWrites) != 3 {
		t.Errorf("total writes = %d, want 3; writes: %+v", len(allWrites), allWrites)
	}

	blockedCount := 0
	for _, w := range allWrites {
		if w.Relation == "blocked" {
			blockedCount++
		}
	}
	if blockedCount != 2 {
		t.Errorf("blocked tuples = %d, want 2", blockedCount)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	origFile := dataFile
	defer func() { dataFile = origFile }()

	dataFile = "/nonexistent/path/data.json"
	// Should not panic
	Load()
}
