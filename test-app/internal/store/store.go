package store

import (
	"encoding/json"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
)

var (
	Data = &DataStore{
		Dossiers:             make(map[string]*Dossier),
		GuardianshipRequests: []GuardianshipRequest{},
		Guardianships:        make(map[string][]string),
	}
	Mu       sync.RWMutex
	dataFile = "/data/dossiers.json"

	AssignableRelations = []string{"owner", "mandate_holder"}
)

func Load() {
	data, err := os.ReadFile(dataFile)
	if err != nil {
		return
	}
	Mu.Lock()
	defer Mu.Unlock()
	if err := json.Unmarshal(data, Data); err != nil {
		log.Printf("WARNING: failed to unmarshal data file: %v", err)
		return
	}
	if Data.Dossiers == nil {
		Data.Dossiers = make(map[string]*Dossier)
	}
	if Data.Guardianships == nil {
		Data.Guardianships = make(map[string][]string)
	}
}

func Save() {
	Mu.Lock()
	defer Mu.Unlock()
	dir := filepath.Dir(dataFile)
	os.MkdirAll(dir, 0755)
	data, _ := json.MarshalIndent(Data, "", "  ")
	os.WriteFile(dataFile, data, 0644)
}

// RehydrateTuples rebuilds all FGA tuples from persisted data.
// It accepts a write function to avoid importing the fga package directly.
func RehydrateTuples(fgaWrite func(writes []TupleKey, deletes []TupleKey) error) {
	var writes []TupleKey
	for id, dossier := range Data.Dossiers {
		writes = append(writes, TupleKey{User: "user:" + dossier.Owner, Relation: "owner", Object: "dossier:" + id})
		for _, rel := range dossier.Relations {
			writes = append(writes, TupleKey{User: "user:" + rel.User, Relation: rel.Relation, Object: "dossier:" + id})
		}
	}
	for userId, guardianList := range Data.Guardianships {
		for _, guardianId := range guardianList {
			writes = append(writes, TupleKey{User: "user:" + guardianId, Relation: "guardian", Object: "user:" + userId})
		}
	}
	for i := 0; i < len(writes); i += 10 {
		end := i + 10
		if end > len(writes) {
			end = len(writes)
		}
		if err := fgaWrite(writes[i:end], nil); err != nil {
			log.Printf("Rehydrate batch error: %v", err)
		}
	}
	if len(writes) > 0 {
		log.Printf("Rehydrated %d tuples from persisted data", len(writes))
	}
}

func RandId() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}
