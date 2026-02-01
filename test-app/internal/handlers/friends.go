package handlers

import (
	"net/http"

	"test-app/internal/config"
	"test-app/internal/fga"
	"test-app/internal/httputil"
	"test-app/internal/store"
)

func FriendsList(w http.ResponseWriter, r *http.Request) {
	user := httputil.GetUser(r)
	userFriends := store.Data.Friends[user]
	if userFriends == nil {
		userFriends = []string{}
	}
	var incoming, outgoing []store.FriendRequest
	for _, req := range store.Data.FriendRequests {
		if req.To == user && req.Status == "pending" {
			incoming = append(incoming, req)
		}
		if req.From == user && req.Status == "pending" {
			outgoing = append(outgoing, req)
		}
	}
	if incoming == nil {
		incoming = []store.FriendRequest{}
	}
	if outgoing == nil {
		outgoing = []store.FriendRequest{}
	}
	httputil.JSONResponse(w, map[string]interface{}{"friends": userFriends, "incoming": incoming, "outgoing": outgoing}, 200)
}

func FriendsRequest(w http.ResponseWriter, r *http.Request) {
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
	if httputil.Contains(store.Data.Friends[user], to) {
		httputil.JSONError(w, "Already friends", 400)
		return
	}
	for _, req := range store.Data.FriendRequests {
		if ((req.From == user && req.To == to) || (req.From == to && req.To == user)) && req.Status == "pending" {
			httputil.JSONError(w, "Request already pending", 400)
			return
		}
	}
	id := store.RandId()
	store.Mu.Lock()
	store.Data.FriendRequests = append(store.Data.FriendRequests, store.FriendRequest{Id: id, From: user, To: to, Status: "pending"})
	store.Mu.Unlock()
	store.Save()
	httputil.JSONResponse(w, map[string]interface{}{"success": true, "id": id}, 200)
}

func FriendsAccept(w http.ResponseWriter, r *http.Request, reqId string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)
	var found *store.FriendRequest
	for i := range store.Data.FriendRequests {
		if store.Data.FriendRequests[i].Id == reqId {
			found = &store.Data.FriendRequests[i]
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
	store.Mu.Lock()
	found.Status = "accepted"
	if store.Data.Friends[user] == nil {
		store.Data.Friends[user] = []string{}
	}
	if store.Data.Friends[found.From] == nil {
		store.Data.Friends[found.From] = []string{}
	}
	store.Data.Friends[user] = append(store.Data.Friends[user], found.From)
	store.Data.Friends[found.From] = append(store.Data.Friends[found.From], user)
	store.Mu.Unlock()
	store.Save()

	fga.Write([]store.TupleKey{
		{User: "user:" + found.From, Relation: "friend", Object: "user:" + user},
		{User: "user:" + user, Relation: "friend", Object: "user:" + found.From},
	}, nil)

	httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
}

func FriendsDeny(w http.ResponseWriter, r *http.Request, reqId string) {
	user := httputil.GetUser(r)
	for i := range store.Data.FriendRequests {
		if store.Data.FriendRequests[i].Id == reqId {
			if store.Data.FriendRequests[i].To != user {
				httputil.JSONError(w, "Not your request to deny", 403)
				return
			}
			store.Data.FriendRequests[i].Status = "denied"
			store.Save()
			httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
			return
		}
	}
	httputil.JSONError(w, "Request not found", 404)
}

func FriendsRemove(w http.ResponseWriter, r *http.Request, userId string) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	user := httputil.GetUser(r)
	store.Mu.Lock()
	if friends, ok := store.Data.Friends[user]; ok {
		var filtered []string
		for _, f := range friends {
			if f != userId {
				filtered = append(filtered, f)
			}
		}
		store.Data.Friends[user] = filtered
	}
	if friends, ok := store.Data.Friends[userId]; ok {
		var filtered []string
		for _, f := range friends {
			if f != user {
				filtered = append(filtered, f)
			}
		}
		store.Data.Friends[userId] = filtered
	}
	store.Mu.Unlock()
	store.Save()

	fga.Write(nil, []store.TupleKey{
		{User: "user:" + userId, Relation: "friend", Object: "user:" + user},
		{User: "user:" + user, Relation: "friend", Object: "user:" + userId},
	})

	httputil.JSONResponse(w, map[string]bool{"success": true}, 200)
}
