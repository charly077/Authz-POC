package store

type Animal struct {
	Name      string     `json:"name"`
	Species   string     `json:"species"`
	Age       int        `json:"age"`
	Owner     string     `json:"owner"`
	ParentId  string     `json:"parentId,omitempty"`
	Relations []Relation `json:"relations,omitempty"`
}

type Relation struct {
	User     string `json:"user"`
	Relation string `json:"relation"`
}

type FriendRequest struct {
	Id     string `json:"id"`
	From   string `json:"from"`
	To     string `json:"to"`
	Status string `json:"status"`
}

type DataStore struct {
	Animals        map[string]*Animal  `json:"animals"`
	FriendRequests []FriendRequest     `json:"friendRequests"`
	Friends        map[string][]string `json:"friends"`
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
