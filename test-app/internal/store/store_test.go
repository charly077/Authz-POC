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
		Animals:        make(map[string]*Animal),
		FriendRequests: []FriendRequest{},
		Friends:        make(map[string][]string),
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
		Animals: map[string]*Animal{
			"a1": {Name: "Rex", Owner: "alice", ParentId: "a0", Relations: []Relation{
				{User: "bob", Relation: "editor"},
			}},
		},
		FriendRequests: []FriendRequest{},
		Friends: map[string][]string{
			"alice": {"bob"},
		},
	}

	var allWrites []TupleKey
	fgaWrite := func(writes []TupleKey, deletes []TupleKey) error {
		allWrites = append(allWrites, writes...)
		return nil
	}
	RehydrateTuples(fgaWrite)

	// Expect: owner tuple, parent tuple, editor relation, friend tuple = 4
	if len(allWrites) != 4 {
		t.Errorf("total writes = %d, want 4", len(allWrites))
	}
}

func TestRehydrateTuples_BatchSplitting(t *testing.T) {
	origData := Data
	defer func() { Data = origData }()

	animals := make(map[string]*Animal)
	for i := 0; i < 12; i++ {
		id := RandId()
		animals[id] = &Animal{Name: "pet", Owner: "alice"}
	}
	Data = &DataStore{
		Animals:        animals,
		FriendRequests: []FriendRequest{},
		Friends:        make(map[string][]string),
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
	dataFile = filepath.Join(tmpDir, "data", "animals.json")

	Data = &DataStore{
		Animals: map[string]*Animal{
			"x1": {Name: "Spot", Species: "Dog", Age: 3, Owner: "alice"},
		},
		FriendRequests: []FriendRequest{{Id: "r1", From: "alice", To: "bob", Status: "pending"}},
		Friends:        map[string][]string{"alice": {"bob"}},
	}

	Save()

	raw, err := os.ReadFile(dataFile)
	if err != nil {
		t.Fatalf("saved file not found: %v", err)
	}

	Data = &DataStore{
		Animals:        make(map[string]*Animal),
		FriendRequests: []FriendRequest{},
		Friends:        make(map[string][]string),
	}
	Load()

	if len(Data.Animals) != 1 {
		t.Fatalf("Animals count = %d, want 1", len(Data.Animals))
	}
	if Data.Animals["x1"].Name != "Spot" {
		t.Errorf("Animal name = %q, want %q", Data.Animals["x1"].Name, "Spot")
	}
	if len(Data.FriendRequests) != 1 {
		t.Errorf("FriendRequests count = %d, want 1", len(Data.FriendRequests))
	}
	if len(Data.Friends["alice"]) != 1 {
		t.Errorf("Friends[alice] count = %d, want 1", len(Data.Friends["alice"]))
	}

	// Verify JSON is valid
	var check DataStore
	if err := json.Unmarshal(raw, &check); err != nil {
		t.Errorf("saved JSON invalid: %v", err)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	origFile := dataFile
	defer func() { dataFile = origFile }()

	dataFile = "/nonexistent/path/data.json"
	// Should not panic
	Load()
}
