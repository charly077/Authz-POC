package fga

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"test-app/internal/audit"
	"test-app/internal/config"
	"test-app/internal/store"
)

func Request(method, path string, body interface{}) (map[string]interface{}, error) {
	var reqBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, config.OpenfgaURL+path, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode FGA response: %w", err)
	}
	return result, nil
}

func Write(writes []store.TupleKey, deletes []store.TupleKey) error {
	body := map[string]interface{}{}
	if len(writes) > 0 {
		body["writes"] = map[string]interface{}{"tuple_keys": writes}
	}
	if len(deletes) > 0 {
		body["deletes"] = map[string]interface{}{"tuple_keys": deletes}
	}
	_, err := Request("POST", "/stores/"+config.FgaStoreId+"/write", body)
	if err == nil {
		for _, t := range writes {
			audit.SendAuditLog("OpenFGA", "write", t.User, t.Relation, t.Object, "WRITE", "Tuple added: "+t.User+" "+t.Relation+" "+t.Object)
		}
		for _, t := range deletes {
			audit.SendAuditLog("OpenFGA", "delete", t.User, t.Relation, t.Object, "WRITE", "Tuple deleted: "+t.User+" "+t.Relation+" "+t.Object)
		}
	}
	return err
}

func Check(user, relation, object string) bool {
	body := map[string]interface{}{
		"tuple_key":              map[string]string{"user": user, "relation": relation, "object": object},
		"authorization_model_id": config.FgaModelId,
	}
	result, err := Request("POST", "/stores/"+config.FgaStoreId+"/check", body)
	if err != nil {
		audit.SendAuditLog("OpenFGA", "deny", user, relation, object, "CHECK", "Error: "+err.Error())
		return false
	}
	allowed, _ := result["allowed"].(bool)
	decision := "deny"
	reason := user + " does not have " + relation + " on " + object
	if allowed {
		decision = "allow"
		reason = user + " has " + relation + " on " + object
	}
	audit.SendAuditLog("OpenFGA", decision, user, relation, object, "CHECK", reason)
	return allowed
}

func ListObjects(user, relation, typeName string) []string {
	body := map[string]interface{}{
		"user":                   user,
		"relation":               relation,
		"type":                   typeName,
		"authorization_model_id": config.FgaModelId,
	}
	result, err := Request("POST", "/stores/"+config.FgaStoreId+"/list-objects", body)
	if err != nil {
		audit.SendAuditLog("OpenFGA", "deny", user, relation, typeName+":*", "LIST", "Error: "+err.Error())
		return nil
	}
	objects, ok := result["objects"].([]interface{})
	if !ok {
		audit.SendAuditLog("OpenFGA", "allow", user, relation, typeName+":*", "LIST", fmt.Sprintf("Listed 0 %s objects", typeName))
		return nil
	}
	var out []string
	for _, o := range objects {
		if s, ok := o.(string); ok {
			out = append(out, s)
		}
	}
	audit.SendAuditLog("OpenFGA", "allow", user, relation, typeName+":*", "LIST", fmt.Sprintf("Listed %d %s objects", len(out), typeName))
	return out
}

func LoadConfig() {
	configPath := "/shared/openfga-store.json"
	for attempt := 1; attempt <= 30; attempt++ {
		data, err := os.ReadFile(configPath)
		if err == nil {
			var cfg store.FgaConfig
			if unmarshalErr := json.Unmarshal(data, &cfg); unmarshalErr != nil {
				log.Printf("WARNING: failed to parse FGA config: %v", unmarshalErr)
			} else if cfg.StoreId != "" && cfg.ModelId != "" {
				config.FgaStoreId = cfg.StoreId
				config.FgaModelId = cfg.ModelId
				config.FgaReady = true
				log.Printf("Loaded OpenFGA config: store=%s model=%s", config.FgaStoreId, config.FgaModelId)
				return
			}
		}
		log.Printf("Waiting for OpenFGA config (%d/30)...", attempt)
		time.Sleep(3 * time.Second)
	}
	log.Println("WARNING: Could not load OpenFGA config after 30 attempts")
}
