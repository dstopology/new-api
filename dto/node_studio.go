package dto

type NodeStudioHandoffUser struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	AccessToken string `json:"access_token"`
}

type NodeStudioHandoffAPIKey struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	APIKey string `json:"api_key"`
}

type NodeStudioHandoffPayload struct {
	Version    int                       `json:"version"`
	IssuedAt   int64                     `json:"issued_at"`
	ExpiresAt  int64                     `json:"expires_at"`
	APIBaseURL string                    `json:"api_base_url"`
	User       NodeStudioHandoffUser     `json:"user"`
	APIKeys    []NodeStudioHandoffAPIKey `json:"api_keys"`
}
