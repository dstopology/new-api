package system_setting

import "testing"

func TestValidateNodeStudioURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "https", url: "https://node.example.com/auth/import-keys"},
		{name: "local http", url: "http://localhost:3000/auth/import-keys"},
		{name: "missing host", url: "/auth/import-keys", wantErr: true},
		{name: "unsupported scheme", url: "javascript:alert(1)", wantErr: true},
		{name: "credentials", url: "https://user:pass@node.example.com/import", wantErr: true},
		{name: "fragment", url: "https://node.example.com/import#payload", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateNodeStudioURL(test.url)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateNodeStudioURL(%q) error = %v, wantErr = %v", test.url, err, test.wantErr)
			}
		})
	}
}
