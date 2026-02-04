package handlers

import (
	"net/http"

	"test-app/internal/config"
	"test-app/internal/fga"
	"test-app/internal/httputil"
	"test-app/internal/store"
)

// isManagerAdmin checks if the request comes from the AI Manager with admin privileges
func isManagerAdmin(r *http.Request) bool {
	return r.Header.Get("x-manager-admin") == "true"
}

func OrganizationsList(w http.ResponseWriter, r *http.Request) {
	store.Mu.RLock()
	orgs := make([]map[string]interface{}, 0, len(store.Data.Organizations))
	for id, org := range store.Data.Organizations {
		orgs = append(orgs, map[string]interface{}{
			"id":      id,
			"name":    org.Name,
			"members": org.Members,
			"admins":  org.Admins,
		})
	}
	store.Mu.RUnlock()
	httputil.JSONResponse(w, map[string]interface{}{"organizations": orgs}, 200)
}

func OrganizationsCreate(w http.ResponseWriter, r *http.Request) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
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

	creator := httputil.GetUser(r)

	membersRaw, _ := body["members"].([]interface{})
	var members []string
	for _, m := range membersRaw {
		if s, ok := m.(string); ok && s != "" {
			members = append(members, s)
		}
	}

	// Ensure creator is always a member
	if !httputil.Contains(members, creator) {
		members = append(members, creator)
	}

	admins := []string{creator}

	id := store.RandId()
	org := &store.Organization{Name: name, Members: members, Admins: admins}

	store.Mu.Lock()
	store.Data.Organizations[id] = org
	store.Mu.Unlock()

	var tuples []store.TupleKey
	for _, member := range members {
		tuples = append(tuples, store.TupleKey{User: "user:" + member, Relation: "member", Object: "organization:" + id})
	}
	tuples = append(tuples, store.TupleKey{User: "user:" + creator, Relation: "admin", Object: "organization:" + id})

	if err := fga.Write(tuples, nil); err != nil {
		store.Mu.Lock()
		delete(store.Data.Organizations, id)
		store.Mu.Unlock()
		httputil.JSONError(w, err.Error(), 500)
		return
	}

	store.Save()
	httputil.JSONResponse(w, map[string]interface{}{
		"id":      id,
		"name":    name,
		"members": members,
		"admins":  admins,
	}, 200)
}

func OrganizationsAddMember(w http.ResponseWriter, r *http.Request, orgId string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}

	currentUser := httputil.GetUser(r)
	if !isManagerAdmin(r) && !fga.Check("user:"+currentUser, "can_manage", "organization:"+orgId) {
		httputil.JSONError(w, "Forbidden: only admins can manage members", 403)
		return
	}

	body, err := httputil.ReadBody(r)
	if err != nil {
		httputil.JSONError(w, "Invalid request body", 400)
		return
	}
	member := httputil.GetString(body, "member")
	if member == "" {
		httputil.JSONError(w, "member is required", 400)
		return
	}

	store.Mu.Lock()
	org, ok := store.Data.Organizations[orgId]
	if !ok {
		store.Mu.Unlock()
		httputil.JSONError(w, "Organization not found", 404)
		return
	}
	if httputil.Contains(org.Members, member) {
		store.Mu.Unlock()
		httputil.JSONError(w, "Already a member", 400)
		return
	}
	prevMembers := make([]string, len(org.Members))
	copy(prevMembers, org.Members)
	org.Members = append(org.Members, member)
	store.Mu.Unlock()

	if err := fga.Write([]store.TupleKey{{User: "user:" + member, Relation: "member", Object: "organization:" + orgId}}, nil); err != nil {
		store.Mu.Lock()
		org.Members = prevMembers
		store.Mu.Unlock()
		httputil.JSONError(w, err.Error(), 500)
		return
	}

	store.Save()
	httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
}

func OrganizationsRemoveMember(w http.ResponseWriter, r *http.Request, orgId string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}

	currentUser := httputil.GetUser(r)
	if !isManagerAdmin(r) && !fga.Check("user:"+currentUser, "can_manage", "organization:"+orgId) {
		httputil.JSONError(w, "Forbidden: only admins can manage members", 403)
		return
	}

	body, err := httputil.ReadBody(r)
	if err != nil {
		httputil.JSONError(w, "Invalid request body", 400)
		return
	}
	member := httputil.GetString(body, "member")
	if member == "" {
		httputil.JSONError(w, "member is required", 400)
		return
	}

	store.Mu.Lock()
	org, ok := store.Data.Organizations[orgId]
	if !ok {
		store.Mu.Unlock()
		httputil.JSONError(w, "Organization not found", 404)
		return
	}
	prevMembers := make([]string, len(org.Members))
	copy(prevMembers, org.Members)
	filtered := make([]string, 0, len(org.Members))
	for _, m := range org.Members {
		if m != member {
			filtered = append(filtered, m)
		}
	}
	org.Members = filtered
	store.Mu.Unlock()

	if err := fga.Write(nil, []store.TupleKey{{User: "user:" + member, Relation: "member", Object: "organization:" + orgId}}); err != nil {
		store.Mu.Lock()
		org.Members = prevMembers
		store.Mu.Unlock()
		httputil.JSONError(w, err.Error(), 500)
		return
	}
	store.Save()
	httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
}

func OrganizationsAddAdmin(w http.ResponseWriter, r *http.Request, orgId string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}

	currentUser := httputil.GetUser(r)
	if !isManagerAdmin(r) && !fga.Check("user:"+currentUser, "can_manage", "organization:"+orgId) {
		httputil.JSONError(w, "Forbidden: only admins can manage admins", 403)
		return
	}

	body, err := httputil.ReadBody(r)
	if err != nil {
		httputil.JSONError(w, "Invalid request body", 400)
		return
	}
	user := httputil.GetString(body, "user")
	if user == "" {
		httputil.JSONError(w, "user is required", 400)
		return
	}

	store.Mu.Lock()
	org, ok := store.Data.Organizations[orgId]
	if !ok {
		store.Mu.Unlock()
		httputil.JSONError(w, "Organization not found", 404)
		return
	}
	if httputil.Contains(org.Admins, user) {
		store.Mu.Unlock()
		httputil.JSONError(w, "Already an admin", 400)
		return
	}

	prevAdmins := make([]string, len(org.Admins))
	copy(prevAdmins, org.Admins)
	prevMembers := make([]string, len(org.Members))
	copy(prevMembers, org.Members)

	org.Admins = append(org.Admins, user)
	isMember := httputil.Contains(org.Members, user)
	if !isMember {
		org.Members = append(org.Members, user)
	}
	store.Mu.Unlock()

	var tuples []store.TupleKey
	tuples = append(tuples, store.TupleKey{User: "user:" + user, Relation: "admin", Object: "organization:" + orgId})
	if !isMember {
		tuples = append(tuples, store.TupleKey{User: "user:" + user, Relation: "member", Object: "organization:" + orgId})
	}

	if err := fga.Write(tuples, nil); err != nil {
		store.Mu.Lock()
		org.Admins = prevAdmins
		org.Members = prevMembers
		store.Mu.Unlock()
		httputil.JSONError(w, err.Error(), 500)
		return
	}

	store.Save()
	httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
}

func OrganizationsRemoveAdmin(w http.ResponseWriter, r *http.Request, orgId string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}

	currentUser := httputil.GetUser(r)
	if !isManagerAdmin(r) && !fga.Check("user:"+currentUser, "can_manage", "organization:"+orgId) {
		httputil.JSONError(w, "Forbidden: only admins can manage admins", 403)
		return
	}

	body, err := httputil.ReadBody(r)
	if err != nil {
		httputil.JSONError(w, "Invalid request body", 400)
		return
	}
	user := httputil.GetString(body, "user")
	if user == "" {
		httputil.JSONError(w, "user is required", 400)
		return
	}

	store.Mu.Lock()
	org, ok := store.Data.Organizations[orgId]
	if !ok {
		store.Mu.Unlock()
		httputil.JSONError(w, "Organization not found", 404)
		return
	}

	// Prevent removing the last admin
	if len(org.Admins) == 1 && httputil.Contains(org.Admins, user) {
		store.Mu.Unlock()
		httputil.JSONError(w, "Cannot remove the last admin. Add another admin first or delete the organization.", 400)
		return
	}

	prevAdmins := make([]string, len(org.Admins))
	copy(prevAdmins, org.Admins)
	filtered := make([]string, 0, len(org.Admins))
	for _, a := range org.Admins {
		if a != user {
			filtered = append(filtered, a)
		}
	}
	org.Admins = filtered
	store.Mu.Unlock()

	if err := fga.Write(nil, []store.TupleKey{{User: "user:" + user, Relation: "admin", Object: "organization:" + orgId}}); err != nil {
		store.Mu.Lock()
		org.Admins = prevAdmins
		store.Mu.Unlock()
		httputil.JSONError(w, err.Error(), 500)
		return
	}

	store.Save()
	httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
}

func OrganizationsDelete(w http.ResponseWriter, r *http.Request, orgId string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}

	currentUser := httputil.GetUser(r)
	if !isManagerAdmin(r) && !fga.Check("user:"+currentUser, "can_manage", "organization:"+orgId) {
		httputil.JSONError(w, "Forbidden: only admins can delete organizations", 403)
		return
	}

	store.Mu.Lock()
	org, ok := store.Data.Organizations[orgId]
	if !ok {
		store.Mu.Unlock()
		httputil.JSONError(w, "Organization not found", 404)
		return
	}

	// Store copies for rollback and FGA tuple deletion
	members := make([]string, len(org.Members))
	copy(members, org.Members)
	admins := make([]string, len(org.Admins))
	copy(admins, org.Admins)
	orgCopy := &store.Organization{Name: org.Name, Members: members, Admins: admins}

	// Find all dossiers linked to this organization
	var affectedDossiers []string
	for dossId, dossier := range store.Data.Dossiers {
		if dossier.OrgId == orgId {
			affectedDossiers = append(affectedDossiers, dossId)
			dossier.OrgId = "" // Clear the org reference
		}
	}

	// Delete from data store
	delete(store.Data.Organizations, orgId)
	store.Mu.Unlock()

	// Build tuples to delete (all member, admin, and org_parent relations)
	var deleteTuples []store.TupleKey
	for _, member := range members {
		deleteTuples = append(deleteTuples, store.TupleKey{User: "user:" + member, Relation: "member", Object: "organization:" + orgId})
	}
	for _, admin := range admins {
		deleteTuples = append(deleteTuples, store.TupleKey{User: "user:" + admin, Relation: "admin", Object: "organization:" + orgId})
	}
	// Delete org_parent tuples for affected dossiers
	for _, dossId := range affectedDossiers {
		deleteTuples = append(deleteTuples, store.TupleKey{User: "organization:" + orgId, Relation: "org_parent", Object: "dossier:" + dossId})
	}

	if err := fga.Write(nil, deleteTuples); err != nil {
		// Rollback: restore organization and dossier org references
		store.Mu.Lock()
		store.Data.Organizations[orgId] = orgCopy
		for _, dossId := range affectedDossiers {
			if dossier, ok := store.Data.Dossiers[dossId]; ok {
				dossier.OrgId = orgId
			}
		}
		store.Mu.Unlock()
		httputil.JSONError(w, err.Error(), 500)
		return
	}

	store.Save()
	httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
}
