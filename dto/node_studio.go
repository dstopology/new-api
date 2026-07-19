package dto

type NodeStudioHandoffUser struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	AccessToken string `json:"access_token"`
}

type NodeStudioHandoffPayload struct {
	Version    int                   `json:"version"`
	IssuedAt   int64                 `json:"issued_at"`
	ExpiresAt  int64                 `json:"expires_at"`
	APIBaseURL string                `json:"api_base_url"`
	User       NodeStudioHandoffUser `json:"user"`
}
