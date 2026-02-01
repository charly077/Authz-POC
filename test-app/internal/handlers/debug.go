package handlers

import (
	"fmt"
	"net/http"

	"test-app/internal/config"
	"test-app/internal/fga"
	"test-app/internal/httputil"
)

func DebugTuples(w http.ResponseWriter, r *http.Request) {
	if !config.FgaReady {
		httputil.JSONError(w, "OpenFGA not ready", 503)
		return
	}
	result, err := fga.Request("POST", "/stores/"+config.FgaStoreId+"/read", map[string]interface{}{})
	if err != nil {
		httputil.JSONError(w, err.Error(), 500)
		return
	}
	tuples, _ := result["tuples"].([]interface{})
	var keys []map[string]string
	for _, t := range tuples {
		tm, _ := t.(map[string]interface{})
		key, _ := tm["key"].(map[string]interface{})
		keys = append(keys, map[string]string{
			"user":     fmt.Sprintf("%v", key["user"]),
			"relation": fmt.Sprintf("%v", key["relation"]),
			"object":   fmt.Sprintf("%v", key["object"]),
		})
	}
	if keys == nil {
		keys = []map[string]string{}
	}
	httputil.JSONResponse(w, map[string]interface{}{"tuples": keys}, 200)
}
