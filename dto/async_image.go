package dto

type AsyncImageTaskError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

// AsyncImageTaskResponse mirrors the upstream async image contract while
// exposing only the gateway's public task ID and temporary local media URLs.
type AsyncImageTaskResponse struct {
	ID            string               `json:"id"`
	Object        string               `json:"object"`
	Model         string               `json:"model"`
	Status        string               `json:"status"`
	Progress      string               `json:"progress"`
	CreatedAt     int64                `json:"created_at"`
	CompletedAt   int64                `json:"completed_at,omitempty"`
	ExpiresAt     int64                `json:"expires_at,omitempty"`
	Data          []ImageData          `json:"data,omitempty"`
	Error         *AsyncImageTaskError `json:"error,omitempty"`
	OutputExpired bool                 `json:"output_expired,omitempty"`
}
