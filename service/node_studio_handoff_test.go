package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func decryptNodeStudioHandoffForTest(t *testing.T, encrypted string, secret string) dto.NodeStudioHandoffPayload {
	t.Helper()

	parts := strings.Split(encrypted, ".")
	if len(parts) != 3 || parts[0] != NodeStudioHandoffVersionPrefix {
		t.Fatalf("unexpected encrypted handoff format: %q", encrypted)
	}
	nonce, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode nonce: %v", err)
	}
	sealed, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode ciphertext: %v", err)
	}

	key := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatalf("create cipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("create GCM: %v", err)
	}
	plaintext, err := gcm.Open(nil, nonce, sealed, []byte(NodeStudioHandoffAAD))
	if err != nil {
		t.Fatalf("decrypt handoff: %v", err)
	}

	var payload dto.NodeStudioHandoffPayload
	if err = common.Unmarshal(plaintext, &payload); err != nil {
		t.Fatalf("unmarshal handoff: %v", err)
	}
	return payload
}

func TestEncryptNodeStudioHandoff(t *testing.T) {
	payload := &dto.NodeStudioHandoffPayload{
		Version:    NodeStudioHandoffVersion,
		IssuedAt:   100,
		ExpiresAt:  220,
		APIBaseURL: "https://api.example.com",
		User: dto.NodeStudioHandoffUser{
			ID:          42,
			Username:    "测试用户",
			AccessToken: "user-secret",
		},
	}

	encrypted, err := EncryptNodeStudioHandoff(payload, " shared-secret ")
	if err != nil {
		t.Fatalf("EncryptNodeStudioHandoff() error = %v", err)
	}
	decrypted := decryptNodeStudioHandoffForTest(t, encrypted, "shared-secret")

	if decrypted.User.Username != payload.User.Username {
		t.Fatalf("username = %q, want %q", decrypted.User.Username, payload.User.Username)
	}
	serialized, err := common.Marshal(decrypted)
	if err != nil {
		t.Fatalf("marshal decrypted handoff: %v", err)
	}
	if strings.Contains(string(serialized), "api_keys") {
		t.Fatalf("account-only handoff unexpectedly contains api_keys: %s", serialized)
	}

	second, err := EncryptNodeStudioHandoff(payload, "shared-secret")
	if err != nil {
		t.Fatalf("second EncryptNodeStudioHandoff() error = %v", err)
	}
	if second == encrypted {
		t.Fatal("encrypted handoffs must use independent nonces")
	}
}

func TestEncryptNodeStudioHandoffRejectsMissingInput(t *testing.T) {
	if _, err := EncryptNodeStudioHandoff(nil, "secret"); err == nil {
		t.Fatal("expected nil payload error")
	}
	if _, err := EncryptNodeStudioHandoff(&dto.NodeStudioHandoffPayload{Version: NodeStudioHandoffVersion}, "  "); err == nil {
		t.Fatal("expected empty secret error")
	}
	if _, err := EncryptNodeStudioHandoff(&dto.NodeStudioHandoffPayload{Version: 1}, "secret"); err == nil {
		t.Fatal("expected unsupported version error")
	}
}
