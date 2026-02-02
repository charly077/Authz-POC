package store

type Dossier struct {
	Title     string     `json:"title"`
	Content   string     `json:"content"`
	Type      string     `json:"type"`
	Owner     string     `json:"owner"`
	Relations []Relation `json:"relations,omitempty"`
}

type Relation struct {
	User     string `json:"user"`
	Relation string `json:"relation"`
}

type GuardianshipRequest struct {
	Id     string `json:"id"`
	From   string `json:"from"`
	To     string `json:"to"`
	Status string `json:"status"`
}

type DataStore struct {
	Dossiers             map[string]*Dossier  `json:"dossiers"`
	GuardianshipRequests []GuardianshipRequest `json:"guardianshipRequests"`
	Guardianships        map[string][]string   `json:"guardianships"`
}

type TupleKey struct {
	User     string `json:"user"`
	Relation string `json:"relation"`
	Object   string `json:"object"`
}

type FgaConfig struct {
	StoreId string `json:"storeId"`
	ModelId string `json:"modelId"`
}
