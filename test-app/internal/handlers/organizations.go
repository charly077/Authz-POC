package handlers

import (
	"net/http"

	"test-app/internal/config"
	"test-app/internal/fga"
	"test-app/internal/httputil"
	"test-app/internal/store"
)

func OrganizationsList(w http.ResponseWriter, r *http.Request) {
	store.Mu.RLock()
	orgs := make([]map[string]interface{}, 0, len(store.Data.Organizations))
	for id, org := range store.Data.Organizations {
		orgs = append(orgs, map[string]interface{}{
			"id":      id,
			"name":    org.Name,
			"members": org.Members,
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

	membersRaw, _ := body["members"].([]interface{})
	var members []string
	for _, m := range membersRaw {
		if s, ok := m.(string); ok && s != "" {
			members = append(members, s)
		}
	}

	id := store.RandId()
	org := &store.Organization{Name: name, Members: members}

	store.Mu.Lock()
	store.Data.Organizations[id] = org
	store.Mu.Unlock()

	var tuples []store.TupleKey
	for _, member := range members {
		tuples = append(tuples, store.TupleKey{User: "user:" + member, Relation: "member", Object: "organization:" + id})
	}
	if len(tuples) > 0 {
		if err := fga.Write(tuples, nil); err != nil {
			store.Mu.Lock()
			delete(store.Data.Organizations, id)
			store.Mu.Unlock()
			httputil.JSONError(w, err.Error(), 500)
			return
		}
	}

	store.Save()
	httputil.JSONResponse(w, map[string]interface{}{"id": id, "name": name, "members": members}, 200)
}

func OrganizationsAddMember(w http.ResponseWriter, r *http.Request, orgId string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
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
