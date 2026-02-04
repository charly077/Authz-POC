package handlers

import (
	"net/http"

	"test-app/internal/config"
	"test-app/internal/fga"
	"test-app/internal/httputil"
	"test-app/internal/store"
)

func GuardianshipsList(w http.ResponseWriter, r *http.Request) {
	user := httputil.GetUser(r)

	// Guardians: people who guard me (stored as Guardianships[me] = [...guardians])
	guardians := store.Data.Guardianships[user]
	if guardians == nil {
		guardians = []string{}
	}

	// Wards: people I guard (I appear in their guardian list)
	var wards []string
	for userId, guardianList := range store.Data.Guardianships {
		if userId == user {
			continue
		}
		if httputil.Contains(guardianList, user) {
			wards = append(wards, userId)
		}
	}
	if wards == nil {
		wards = []string{}
	}

	var incoming, outgoing []store.GuardianshipRequest
	for _, req := range store.Data.GuardianshipRequests {
		if req.To == user && req.Status == "pending" {
			incoming = append(incoming, req)
		}
		if req.From == user && req.Status == "pending" {
			outgoing = append(outgoing, req)
		}
	}
	if incoming == nil {
		incoming = []store.GuardianshipRequest{}
	}
	if outgoing == nil {
		outgoing = []store.GuardianshipRequest{}
	}
	httputil.JSONResponse(w, map[string]interface{}{
		"guardians": guardians,
		"wards":     wards,
		"incoming":  incoming,
		"outgoing":  outgoing,
	}, 200)
}

func GuardianshipRequest(w http.ResponseWriter, r *http.Request) {
	user := httputil.GetUser(r)
	body, err := httputil.ReadBody(r)
	if err != nil {
		httputil.JSONError(w, "Invalid request body", 400)
		return
	}
	to := httputil.GetString(body, "to")
	if to == "" || to == user {
		httputil.JSONError(w, "Invalid target user", 400)
		return
	}
	// Check if guardianship already exists in either direction
	if httputil.Contains(store.Data.Guardianships[to], user) {
		httputil.JSONError(w, "Already a guardian of "+to, 400)
		return
	}
	for _, req := range store.Data.GuardianshipRequests {
		if ((req.From == user && req.To == to) || (req.From == to && req.To == user)) && req.Status == "pending" {
			httputil.JSONError(w, "Request already pending", 400)
			return
		}
	}
	id := store.RandId()
	store.Mu.Lock()
	store.Data.GuardianshipRequests = append(store.Data.GuardianshipRequests, store.GuardianshipRequest{Id: id, From: user, To: to, Status: "pending"})
	store.Mu.Unlock()
	store.Save()
	httputil.JSONResponse(w, map[string]interface{}{"success": true, "id": id}, 200)
}

func GuardianshipAccept(w http.ResponseWriter, r *http.Request, reqId string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)
	var found *store.GuardianshipRequest
	for i := range store.Data.GuardianshipRequests {
		if store.Data.GuardianshipRequests[i].Id == reqId {
			found = &store.Data.GuardianshipRequests[i]
			break
		}
	}
	if found == nil {
		httputil.JSONError(w, "Request not found", 404)
		return
	}
	if found.To != user {
		httputil.JSONError(w, "Not your request to accept", 403)
		return
	}
	if found.Status != "pending" {
		httputil.JSONError(w, "Request already handled", 400)
		return
	}
	// Directional: from (requester) becomes guardian of to (accepter)
	// user:from guardian user:to
	store.Mu.Lock()
	found.Status = "accepted"
	if store.Data.Guardianships[user] == nil {
		store.Data.Guardianships[user] = []string{}
	}
	store.Data.Guardianships[user] = append(store.Data.Guardianships[user], found.From)
	store.Mu.Unlock()
	store.Save()

	fga.Write([]store.TupleKey{
		{User: "user:" + found.From, Relation: "guardian", Object: "user:" + user},
	}, nil)

	httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
}

func GuardianshipDeny(w http.ResponseWriter, r *http.Request, reqId string) {
	user := httputil.GetUser(r)
	for i := range store.Data.GuardianshipRequests {
		if store.Data.GuardianshipRequests[i].Id == reqId {
			if store.Data.GuardianshipRequests[i].To != user {
				httputil.JSONError(w, "Not your request to deny", 403)
				return
			}
			store.Data.GuardianshipRequests[i].Status = "denied"
			store.Save()
			httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
			return
		}
	}
	httputil.JSONError(w, "Request not found", 404)
}

func GuardianshipRemove(w http.ResponseWriter, r *http.Request, userId string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)

	var deletes []store.TupleKey

	// Remove from both possible directions
	store.Mu.Lock()
	// If userId is a guardian of user
	if guardians, ok := store.Data.Guardianships[user]; ok {
		found := false
		var filtered []string
		for _, g := range guardians {
			if g == userId {
				found = true
			} else {
				filtered = append(filtered, g)
			}
		}
		if found {
			store.Data.Guardianships[user] = filtered
			deletes = append(deletes, store.TupleKey{User: "user:" + userId, Relation: "guardian", Object: "user:" + user})
		}
	}
	// If user is a guardian of userId
	if guardians, ok := store.Data.Guardianships[userId]; ok {
		found := false
		var filtered []string
		for _, g := range guardians {
			if g == user {
				found = true
			} else {
				filtered = append(filtered, g)
			}
		}
		if found {
			store.Data.Guardianships[userId] = filtered
			deletes = append(deletes, store.TupleKey{User: "user:" + user, Relation: "guardian", Object: "user:" + userId})
		}
	}
	store.Mu.Unlock()
	store.Save()

	if len(deletes) > 0 {
		fga.Write(nil, deletes)
	}

	httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
}
