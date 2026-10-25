package auth

type CreateClientRequest struct {
	Name string `json:"name"`
}

type CreateClientResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Token string `json:"token"`
}

type ClientDTO struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
	Name   string `json:"name"`
}

type ClientListResponse struct {
	Item []ClientDTO `json:"item"`
}
