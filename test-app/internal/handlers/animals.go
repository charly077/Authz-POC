package handlers

import (
	"net/http"
	"strings"

	"test-app/internal/fga"
	"test-app/internal/httputil"
	"test-app/internal/config"
	"test-app/internal/store"
)

func AnimalsList(w http.ResponseWriter, r *http.Request) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)
	visibleIds := fga.ListObjects("user:"+user, "viewer", "animal")

	type animalResp struct {
		Id        string           `json:"id"`
		Name      string           `json:"name"`
		Species   string           `json:"species"`
		Age       int              `json:"age"`
		Owner     string           `json:"owner"`
		CanEdit   bool             `json:"canEdit"`
		Relations []store.Relation `json:"relations,omitempty"`
	}

	store.Mu.RLock()
	var animals []animalResp
	for _, obj := range visibleIds {
		id := strings.TrimPrefix(obj, "animal:")
		a, ok := store.Data.Animals[id]
		if !ok {
			continue
		}
		canEdit := fga.Check("user:"+user, "editor", "animal:"+id)
		animals = append(animals, animalResp{
			Id: id, Name: a.Name, Species: a.Species, Age: a.Age,
			Owner: a.Owner, CanEdit: canEdit, Relations: a.Relations,
		})
	}
	store.Mu.RUnlock()
	if animals == nil {
		animals = []animalResp{}
	}
	httputil.JSONResponse(w, map[string]interface{}{"animals": animals}, 200)
}

func AnimalsCreate(w http.ResponseWriter, r *http.Request) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)
	body, err := httputil.ReadBody(r)
	if err != nil {
		httputil.JSONError(w, "Invalid request body", 400)
		return
	}
	name := httputil.GetString(body, "name")
	if name == "" {
		httputil.JSONError(w, "Name is required", 400)
		return
	}
	species := httputil.GetString(body, "species")
	if species == "" {
		species = "Unknown"
	}
	age := httputil.GetInt(body, "age")

	id := store.RandId()
	animal := &store.Animal{Name: name, Species: species, Age: age, Owner: user}
	store.Mu.Lock()
	store.Data.Animals[id] = animal
	store.Mu.Unlock()
	store.Save()

	err = fga.Write([]store.TupleKey{{User: "user:" + user, Relation: "owner", Object: "animal:" + id}}, nil)
	if err != nil {
		store.Mu.Lock()
		delete(store.Data.Animals, id)
		store.Mu.Unlock()
		store.Save()
		httputil.JSONError(w, err.Error(), 500)
		return
	}
	httputil.JSONResponse(w, map[string]interface{}{"id": id, "name": name, "species": species, "age": age, "owner": user}, 200)
}

func AnimalsUpdate(w http.ResponseWriter, r *http.Request, id string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)
	animal, ok := store.Data.Animals[id]
	if !ok {
		httputil.JSONError(w, "Animal not found", 404)
		return
	}
	if !fga.Check("user:"+user, "editor", "animal:"+id) {
		httputil.JSONError(w, "Not authorized to edit this animal", 403)
		return
	}
	body, err := httputil.ReadBody(r)
	if err != nil {
		httputil.JSONError(w, "Invalid request body", 400)
		return
	}
	if v := httputil.GetString(body, "name"); v != "" {
		animal.Name = v
	}
	if v := httputil.GetString(body, "species"); v != "" {
		animal.Species = v
	}
	if _, ok := body["age"]; ok {
		animal.Age = httputil.GetInt(body, "age")
	}
	store.Save()
	httputil.JSONResponse(w, map[string]interface{}{"id": id, "name": animal.Name, "species": animal.Species, "age": animal.Age, "owner": animal.Owner}, 200)
}

func AnimalsDelete(w http.ResponseWriter, r *http.Request, id string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)
	animal, ok := store.Data.Animals[id]
	if !ok {
		httputil.JSONError(w, "Animal not found", 404)
		return
	}
	if !fga.Check("user:"+user, "editor", "animal:"+id) {
		httputil.JSONError(w, "Not authorized to delete this animal", 403)
		return
	}
	deletes := []store.TupleKey{{User: "user:" + animal.Owner, Relation: "owner", Object: "animal:" + id}}
	if animal.ParentId != "" {
		deletes = append(deletes, store.TupleKey{User: "animal:" + animal.ParentId, Relation: "parent", Object: "animal:" + id})
	}
	for _, rel := range animal.Relations {
		deletes = append(deletes, store.TupleKey{User: "user:" + rel.User, Relation: rel.Relation, Object: "animal:" + id})
	}
	fga.Write(nil, deletes)
	store.Mu.Lock()
	delete(store.Data.Animals, id)
	store.Mu.Unlock()
	store.Save()
	httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
}

func AnimalsRelationsGet(w http.ResponseWriter, r *http.Request, id string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)
	animal, ok := store.Data.Animals[id]
	if !ok {
		httputil.JSONError(w, "Animal not found", 404)
		return
	}
	if !fga.Check("user:"+user, "editor", "animal:"+id) {
		httputil.JSONError(w, "Not authorized", 403)
		return
	}
	rels := animal.Relations
	if rels == nil {
		rels = []store.Relation{}
	}
	httputil.JSONResponse(w, map[string]interface{}{"relations": rels}, 200)
}

func AnimalsRelationsAdd(w http.ResponseWriter, r *http.Request, id string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)
	animal, ok := store.Data.Animals[id]
	if !ok {
		httputil.JSONError(w, "Animal not found", 404)
		return
	}
	body, err := httputil.ReadBody(r)
	if err != nil {
		httputil.JSONError(w, "Invalid request body", 400)
		return
	}
	targetUser := httputil.GetString(body, "targetUser")
	relation := httputil.GetString(body, "relation")
	if targetUser == "" || relation == "" {
		httputil.JSONError(w, "targetUser and relation are required", 400)
		return
	}
	if !httputil.Contains(store.AssignableRelations, relation) {
		httputil.JSONError(w, "Invalid relation", 400)
		return
	}
	if !fga.Check("user:"+user, "editor", "animal:"+id) {
		httputil.JSONError(w, "Not authorized to manage relations on this animal", 403)
		return
	}
	userFriends := store.Data.Friends[user]
	if !httputil.Contains(userFriends, targetUser) {
		httputil.JSONError(w, targetUser+" is not your friend. You can only assign relations to friends.", 400)
		return
	}
	for _, rel := range animal.Relations {
		if rel.User == targetUser && rel.Relation == relation {
			httputil.JSONError(w, "Relation already exists", 400)
			return
		}
	}
	err = fga.Write([]store.TupleKey{{User: "user:" + targetUser, Relation: relation, Object: "animal:" + id}}, nil)
	if err != nil {
		httputil.JSONError(w, err.Error(), 500)
		return
	}
	animal.Relations = append(animal.Relations, store.Relation{User: targetUser, Relation: relation})
	store.Save()
	httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
}

func AnimalsRelationsDelete(w http.ResponseWriter, r *http.Request, id string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)
	animal, ok := store.Data.Animals[id]
	if !ok {
		httputil.JSONError(w, "Animal not found", 404)
		return
	}
	body, err := httputil.ReadBody(r)
	if err != nil {
		httputil.JSONError(w, "Invalid request body", 400)
		return
	}
	targetUser := httputil.GetString(body, "targetUser")
	relation := httputil.GetString(body, "relation")
	if targetUser == "" || relation == "" {
		httputil.JSONError(w, "targetUser and relation are required", 400)
		return
	}
	if !fga.Check("user:"+user, "editor", "animal:"+id) {
		httputil.JSONError(w, "Not authorized", 403)
		return
	}
	fga.Write(nil, []store.TupleKey{{User: "user:" + targetUser, Relation: relation, Object: "animal:" + id}})
	var newRels []store.Relation
	for _, rel := range animal.Relations {
		if !(rel.User == targetUser && rel.Relation == relation) {
			newRels = append(newRels, rel)
		}
	}
	animal.Relations = newRels
	store.Save()
	httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
}
