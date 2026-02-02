package handlers

import (
	"net/http"
	"strings"

	"test-app/internal/config"
	"test-app/internal/fga"
	"test-app/internal/httputil"
	"test-app/internal/store"
)

var validDossierTypes = []string{"tax", "health", "general"}

func DossiersList(w http.ResponseWriter, r *http.Request) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)
	visibleIds := fga.ListObjects("user:"+user, "viewer", "dossier")

	type dossierResp struct {
		Id        string           `json:"id"`
		Title     string           `json:"title"`
		Content   string           `json:"content"`
		Type      string           `json:"type"`
		Owner     string           `json:"owner"`
		CanEdit   bool             `json:"canEdit"`
		Relations []store.Relation `json:"relations,omitempty"`
	}

	store.Mu.RLock()
	var dossiers []dossierResp
	for _, obj := range visibleIds {
		id := strings.TrimPrefix(obj, "dossier:")
		d, ok := store.Data.Dossiers[id]
		if !ok {
			continue
		}
		canEdit := fga.Check("user:"+user, "editor", "dossier:"+id)
		dossiers = append(dossiers, dossierResp{
			Id: id, Title: d.Title, Content: d.Content, Type: d.Type,
			Owner: d.Owner, CanEdit: canEdit, Relations: d.Relations,
		})
	}
	store.Mu.RUnlock()
	if dossiers == nil {
		dossiers = []dossierResp{}
	}
	httputil.JSONResponse(w, map[string]interface{}{"dossiers": dossiers}, 200)
}

func DossiersCreate(w http.ResponseWriter, r *http.Request) {
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
	title := httputil.GetString(body, "title")
	if title == "" {
		httputil.JSONError(w, "Title is required", 400)
		return
	}
	content := httputil.GetString(body, "content")
	dossierType := httputil.GetString(body, "type")
	if !httputil.Contains(validDossierTypes, dossierType) {
		httputil.JSONError(w, "Type must be one of: tax, health, general", 400)
		return
	}

	id := store.RandId()
	dossier := &store.Dossier{Title: title, Content: content, Type: dossierType, Owner: user}
	store.Mu.Lock()
	store.Data.Dossiers[id] = dossier
	store.Mu.Unlock()
	store.Save()

	err = fga.Write([]store.TupleKey{{User: "user:" + user, Relation: "owner", Object: "dossier:" + id}}, nil)
	if err != nil {
		store.Mu.Lock()
		delete(store.Data.Dossiers, id)
		store.Mu.Unlock()
		store.Save()
		httputil.JSONError(w, err.Error(), 500)
		return
	}
	httputil.JSONResponse(w, map[string]interface{}{"id": id, "title": title, "content": content, "type": dossierType, "owner": user}, 200)
}

func DossiersUpdate(w http.ResponseWriter, r *http.Request, id string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)
	dossier, ok := store.Data.Dossiers[id]
	if !ok {
		httputil.JSONError(w, "Dossier not found", 404)
		return
	}
	if !fga.Check("user:"+user, "editor", "dossier:"+id) {
		httputil.JSONError(w, "Not authorized to edit this dossier", 403)
		return
	}
	body, err := httputil.ReadBody(r)
	if err != nil {
		httputil.JSONError(w, "Invalid request body", 400)
		return
	}
	if v := httputil.GetString(body, "title"); v != "" {
		dossier.Title = v
	}
	if v := httputil.GetString(body, "content"); v != "" {
		dossier.Content = v
	}
	if v := httputil.GetString(body, "type"); v != "" {
		if !httputil.Contains(validDossierTypes, v) {
			httputil.JSONError(w, "Type must be one of: tax, health, general", 400)
			return
		}
		dossier.Type = v
	}
	store.Save()
	httputil.JSONResponse(w, map[string]interface{}{"id": id, "title": dossier.Title, "content": dossier.Content, "type": dossier.Type, "owner": dossier.Owner}, 200)
}

func DossiersDelete(w http.ResponseWriter, r *http.Request, id string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)
	dossier, ok := store.Data.Dossiers[id]
	if !ok {
		httputil.JSONError(w, "Dossier not found", 404)
		return
	}
	if !fga.Check("user:"+user, "editor", "dossier:"+id) {
		httputil.JSONError(w, "Not authorized to delete this dossier", 403)
		return
	}
	deletes := []store.TupleKey{{User: "user:" + dossier.Owner, Relation: "owner", Object: "dossier:" + id}}
	for _, rel := range dossier.Relations {
		deletes = append(deletes, store.TupleKey{User: "user:" + rel.User, Relation: rel.Relation, Object: "dossier:" + id})
	}
	fga.Write(nil, deletes)
	store.Mu.Lock()
	delete(store.Data.Dossiers, id)
	store.Mu.Unlock()
	store.Save()
	httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
}

func DossiersRelationsGet(w http.ResponseWriter, r *http.Request, id string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)
	dossier, ok := store.Data.Dossiers[id]
	if !ok {
		httputil.JSONError(w, "Dossier not found", 404)
		return
	}
	if !fga.Check("user:"+user, "editor", "dossier:"+id) {
		httputil.JSONError(w, "Not authorized", 403)
		return
	}
	rels := dossier.Relations
	if rels == nil {
		rels = []store.Relation{}
	}
	httputil.JSONResponse(w, map[string]interface{}{"relations": rels}, 200)
}

func DossiersRelationsAdd(w http.ResponseWriter, r *http.Request, id string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)
	dossier, ok := store.Data.Dossiers[id]
	if !ok {
		httputil.JSONError(w, "Dossier not found", 404)
		return
	}
	body, err := httputil.ReadBody(r)
	if err != nil {
		httputil.JSONError(w, "Invalid request body", 400)
		return
	}
	targetUser := httputil.GetString(body, "targetUser")
	if targetUser == "" {
		httputil.JSONError(w, "targetUser is required", 400)
		return
	}
	if !fga.Check("user:"+user, "editor", "dossier:"+id) {
		httputil.JSONError(w, "Not authorized to manage relations on this dossier", 403)
		return
	}
	// Check guardianship: targetUser must be a guardian of user OR user must be a guardian of targetUser
	userGuardians := store.Data.Guardianships[user]
	targetGuardians := store.Data.Guardianships[targetUser]
	if !httputil.Contains(userGuardians, targetUser) && !httputil.Contains(targetGuardians, user) {
		httputil.JSONError(w, targetUser+" is not in a guardianship with you. You can only grant mandates to guardians or wards.", 400)
		return
	}
	relation := "mandate_holder"
	for _, rel := range dossier.Relations {
		if rel.User == targetUser && rel.Relation == relation {
			httputil.JSONError(w, "Mandate already exists", 400)
			return
		}
	}
	err = fga.Write([]store.TupleKey{{User: "user:" + targetUser, Relation: relation, Object: "dossier:" + id}}, nil)
	if err != nil {
		httputil.JSONError(w, err.Error(), 500)
		return
	}
	dossier.Relations = append(dossier.Relations, store.Relation{User: targetUser, Relation: relation})
	store.Save()
	httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
}

func DossiersRelationsDelete(w http.ResponseWriter, r *http.Request, id string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)
	dossier, ok := store.Data.Dossiers[id]
	if !ok {
		httputil.JSONError(w, "Dossier not found", 404)
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
	if !fga.Check("user:"+user, "editor", "dossier:"+id) {
		httputil.JSONError(w, "Not authorized", 403)
		return
	}
	fga.Write(nil, []store.TupleKey{{User: "user:" + targetUser, Relation: relation, Object: "dossier:" + id}})
	var newRels []store.Relation
	for _, rel := range dossier.Relations {
		if !(rel.User == targetUser && rel.Relation == relation) {
			newRels = append(newRels, rel)
		}
	}
	dossier.Relations = newRels
	store.Save()
	httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
}
