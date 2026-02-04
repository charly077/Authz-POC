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

// isManagerAdminDossiers checks if the request comes from the AI Manager with admin privileges
func isManagerAdminDossiers(r *http.Request) bool {
	return r.Header.Get("x-manager-admin") == "true"
}

// UsersList returns all known users in the system (for admin use)
func UsersList(w http.ResponseWriter, r *http.Request) {
	if !isManagerAdminDossiers(r) {
		httputil.JSONError(w, "Admin access required", 403)
		return
	}

	// Collect users from all sources
	userSet := make(map[string]bool)

	store.Mu.RLock()
	// From dossiers (owners and relations)
	for _, d := range store.Data.Dossiers {
		userSet[d.Owner] = true
		for _, rel := range d.Relations {
			userSet[rel.User] = true
		}
		for _, blocked := range d.BlockedUsers {
			userSet[blocked] = true
		}
	}
	// From guardianships
	for userId, guardians := range store.Data.Guardianships {
		userSet[userId] = true
		for _, g := range guardians {
			userSet[g] = true
		}
	}
	// From guardianship requests
	for _, req := range store.Data.GuardianshipRequests {
		userSet[req.From] = true
		userSet[req.To] = true
	}
	// From organizations
	for _, org := range store.Data.Organizations {
		for _, m := range org.Members {
			userSet[m] = true
		}
		for _, a := range org.Admins {
			userSet[a] = true
		}
	}
	store.Mu.RUnlock()

	var users []string
	for u := range userSet {
		users = append(users, u)
	}

	httputil.JSONResponse(w, map[string]interface{}{"users": users}, 200)
}

// GuardianshipsListAll returns all guardianships in the system (for admin use)
func GuardianshipsListAll(w http.ResponseWriter, r *http.Request) {
	if !isManagerAdminDossiers(r) {
		httputil.JSONError(w, "Admin access required", 403)
		return
	}

	type guardianshipResp struct {
		User      string   `json:"user"`
		Guardians []string `json:"guardians"`
	}

	store.Mu.RLock()
	var guardianships []guardianshipResp
	for userId, guardians := range store.Data.Guardianships {
		guardianships = append(guardianships, guardianshipResp{
			User:      userId,
			Guardians: guardians,
		})
	}
	store.Mu.RUnlock()

	if guardianships == nil {
		guardianships = []guardianshipResp{}
	}
	httputil.JSONResponse(w, map[string]interface{}{"guardianships": guardianships}, 200)
}

// DossiersListAll returns all dossiers (for admin use)
func DossiersListAll(w http.ResponseWriter, r *http.Request) {
	if !isManagerAdminDossiers(r) {
		httputil.JSONError(w, "Admin access required", 403)
		return
	}

	type dossierResp struct {
		Id           string           `json:"id"`
		Title        string           `json:"title"`
		Content      string           `json:"content"`
		Type         string           `json:"type"`
		Owner        string           `json:"owner"`
		Relations    []store.Relation `json:"relations,omitempty"`
		IsPublic     bool             `json:"isPublic"`
		BlockedUsers []string         `json:"blockedUsers,omitempty"`
		OrgId        string           `json:"orgId,omitempty"`
	}

	store.Mu.RLock()
	var dossiers []dossierResp
	for id, d := range store.Data.Dossiers {
		dossiers = append(dossiers, dossierResp{
			Id: id, Title: d.Title, Content: d.Content, Type: d.Type,
			Owner: d.Owner, Relations: d.Relations,
			IsPublic: d.Public, BlockedUsers: d.BlockedUsers, OrgId: d.OrgId,
		})
	}
	store.Mu.RUnlock()
	if dossiers == nil {
		dossiers = []dossierResp{}
	}
	httputil.JSONResponse(w, map[string]interface{}{"dossiers": dossiers}, 200)
}

func DossiersList(w http.ResponseWriter, r *http.Request) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)
	visibleIds := fga.ListObjects("user:"+user, "viewer", "dossier")

	type dossierResp struct {
		Id           string           `json:"id"`
		Title        string           `json:"title"`
		Content      string           `json:"content"`
		Type         string           `json:"type"`
		Owner        string           `json:"owner"`
		CanEdit      bool             `json:"canEdit"`
		Relations    []store.Relation `json:"relations,omitempty"`
		IsPublic     bool             `json:"isPublic"`
		BlockedUsers []string         `json:"blockedUsers,omitempty"`
		OrgId        string           `json:"orgId,omitempty"`
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
			IsPublic: d.Public, BlockedUsers: d.BlockedUsers, OrgId: d.OrgId,
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

	orgId := httputil.GetString(body, "orgId")
	isPublic, _ := body["public"].(bool)

	if orgId != "" {
		store.Mu.RLock()
		_, orgExists := store.Data.Organizations[orgId]
		store.Mu.RUnlock()
		if !orgExists {
			httputil.JSONError(w, "Organization not found", 404)
			return
		}
	}

	id := store.RandId()
	dossier := &store.Dossier{Title: title, Content: content, Type: dossierType, Owner: user, OrgId: orgId, Public: isPublic}
	store.Mu.Lock()
	store.Data.Dossiers[id] = dossier
	store.Mu.Unlock()

	tuples := []store.TupleKey{{User: "user:" + user, Relation: "owner", Object: "dossier:" + id}}
	if orgId != "" {
		tuples = append(tuples, store.TupleKey{User: "organization:" + orgId, Relation: "org_parent", Object: "dossier:" + id})
	}
	if isPublic {
		tuples = append(tuples, store.TupleKey{User: "user:*", Relation: "public", Object: "dossier:" + id})
	}

	err = fga.Write(tuples, nil)
	if err != nil {
		store.Mu.Lock()
		delete(store.Data.Dossiers, id)
		store.Mu.Unlock()
		store.Save()
		httputil.JSONError(w, err.Error(), 500)
		return
	}
	store.Save()
	httputil.JSONResponse(w, map[string]interface{}{"id": id, "title": title, "content": content, "type": dossierType, "owner": user, "orgId": orgId, "isPublic": isPublic}, 200)
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
	if !isManagerAdminDossiers(r) && !fga.Check("user:"+user, "editor", "dossier:"+id) {
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
	if !isManagerAdminDossiers(r) && !fga.Check("user:"+user, "editor", "dossier:"+id) {
		httputil.JSONError(w, "Not authorized to delete this dossier", 403)
		return
	}
	deletes := []store.TupleKey{{User: "user:" + dossier.Owner, Relation: "owner", Object: "dossier:" + id}}
	for _, rel := range dossier.Relations {
		deletes = append(deletes, store.TupleKey{User: "user:" + rel.User, Relation: rel.Relation, Object: "dossier:" + id})
	}
	if dossier.OrgId != "" {
		deletes = append(deletes, store.TupleKey{User: "organization:" + dossier.OrgId, Relation: "org_parent", Object: "dossier:" + id})
	}
	if dossier.Public {
		deletes = append(deletes, store.TupleKey{User: "user:*", Relation: "public", Object: "dossier:" + id})
	}
	for _, blocked := range dossier.BlockedUsers {
		deletes = append(deletes, store.TupleKey{User: "user:" + blocked, Relation: "blocked", Object: "dossier:" + id})
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
	if !isManagerAdminDossiers(r) && !fga.Check("user:"+user, "editor", "dossier:"+id) {
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
	if !isManagerAdminDossiers(r) && !fga.Check("user:"+user, "editor", "dossier:"+id) {
		httputil.JSONError(w, "Not authorized to manage relations on this dossier", 403)
		return
	}
	// Admin can add any relation without guardianship check; regular users need guardianship
	if !isManagerAdminDossiers(r) {
		// Check guardianship: targetUser must be a guardian of user OR user must be a guardian of targetUser
		userGuardians := store.Data.Guardianships[user]
		targetGuardians := store.Data.Guardianships[targetUser]
		if !httputil.Contains(userGuardians, targetUser) && !httputil.Contains(targetGuardians, user) {
			httputil.JSONError(w, targetUser+" is not in a guardianship with you. You can only grant mandates to guardians or wards.", 400)
			return
		}
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
	if !isManagerAdminDossiers(r) && !fga.Check("user:"+user, "editor", "dossier:"+id) {
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

func DossiersTogglePublic(w http.ResponseWriter, r *http.Request, id string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)
	store.Mu.Lock()
	dossier, ok := store.Data.Dossiers[id]
	if !ok {
		store.Mu.Unlock()
		httputil.JSONError(w, "Dossier not found", 404)
		return
	}
	if !isManagerAdminDossiers(r) && dossier.Owner != user {
		store.Mu.Unlock()
		httputil.JSONError(w, "Only the owner can toggle public status", 403)
		return
	}
	wasPublic := dossier.Public
	dossier.Public = !wasPublic
	store.Mu.Unlock()

	tuple := store.TupleKey{User: "user:*", Relation: "public", Object: "dossier:" + id}
	var fgaErr error
	if wasPublic {
		fgaErr = fga.Write(nil, []store.TupleKey{tuple})
	} else {
		fgaErr = fga.Write([]store.TupleKey{tuple}, nil)
	}
	if fgaErr != nil {
		store.Mu.Lock()
		dossier.Public = wasPublic
		store.Mu.Unlock()
		httputil.JSONError(w, fgaErr.Error(), 500)
		return
	}

	store.Save()
	httputil.JSONResponse(w, map[string]interface{}{"success": true, "isPublic": dossier.Public}, 200)
}

func DossiersBlock(w http.ResponseWriter, r *http.Request, id string) {
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
	targetUser := httputil.GetString(body, "targetUser")
	if targetUser == "" {
		httputil.JSONError(w, "targetUser is required", 400)
		return
	}

	store.Mu.Lock()
	dossier, ok := store.Data.Dossiers[id]
	if !ok {
		store.Mu.Unlock()
		httputil.JSONError(w, "Dossier not found", 404)
		return
	}
	if !isManagerAdminDossiers(r) && dossier.Owner != user {
		store.Mu.Unlock()
		httputil.JSONError(w, "Only the owner can block users", 403)
		return
	}
	if httputil.Contains(dossier.BlockedUsers, targetUser) {
		store.Mu.Unlock()
		httputil.JSONError(w, "User already blocked", 400)
		return
	}
	prevBlocked := make([]string, len(dossier.BlockedUsers))
	copy(prevBlocked, dossier.BlockedUsers)
	dossier.BlockedUsers = append(dossier.BlockedUsers, targetUser)
	store.Mu.Unlock()

	if err := fga.Write([]store.TupleKey{{User: "user:" + targetUser, Relation: "blocked", Object: "dossier:" + id}}, nil); err != nil {
		store.Mu.Lock()
		dossier.BlockedUsers = prevBlocked
		store.Mu.Unlock()
		httputil.JSONError(w, err.Error(), 500)
		return
	}

	store.Save()
	httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
}

func DossiersUnblock(w http.ResponseWriter, r *http.Request, id string) {
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
	targetUser := httputil.GetString(body, "targetUser")
	if targetUser == "" {
		httputil.JSONError(w, "targetUser is required", 400)
		return
	}

	store.Mu.Lock()
	dossier, ok := store.Data.Dossiers[id]
	if !ok {
		store.Mu.Unlock()
		httputil.JSONError(w, "Dossier not found", 404)
		return
	}
	if !isManagerAdminDossiers(r) && dossier.Owner != user {
		store.Mu.Unlock()
		httputil.JSONError(w, "Only the owner can unblock users", 403)
		return
	}
	prevBlocked := make([]string, len(dossier.BlockedUsers))
	copy(prevBlocked, dossier.BlockedUsers)
	filtered := make([]string, 0, len(dossier.BlockedUsers))
	for _, b := range dossier.BlockedUsers {
		if b != targetUser {
			filtered = append(filtered, b)
		}
	}
	dossier.BlockedUsers = filtered
	store.Mu.Unlock()

	if err := fga.Write(nil, []store.TupleKey{{User: "user:" + targetUser, Relation: "blocked", Object: "dossier:" + id}}); err != nil {
		store.Mu.Lock()
		dossier.BlockedUsers = prevBlocked
		store.Mu.Unlock()
		httputil.JSONError(w, err.Error(), 500)
		return
	}
	store.Save()
	httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
}

func DossiersEmergencyCheck(w http.ResponseWriter, r *http.Request, id string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	body, err := httputil.ReadBody(r)
	if err != nil {
		httputil.JSONError(w, "Invalid request body", 400)
		return
	}
	targetUser := httputil.GetString(body, "user")
	if targetUser == "" {
		httputil.JSONError(w, "user is required", 400)
		return
	}
	relation := httputil.GetString(body, "relation")
	if relation == "" {
		relation = "viewer"
	}

	store.Mu.RLock()
	_, ok := store.Data.Dossiers[id]
	store.Mu.RUnlock()
	if !ok {
		httputil.JSONError(w, "Dossier not found", 404)
		return
	}

	contextualTuples := []store.TupleKey{
		{User: "user:" + targetUser, Relation: "can_view", Object: "dossier:" + id},
	}

	allowed := fga.CheckWithContext("user:"+targetUser, relation, "dossier:"+id, contextualTuples)
	httputil.JSONResponse(w, map[string]interface{}{"allowed": allowed, "user": targetUser, "relation": relation, "dossier": id, "contextual": true}, 200)
}
