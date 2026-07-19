package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

const (
	NodeStudioHandoffVersion       = 2
	NodeStudioHandoffVersionPrefix = "v2"
	NodeStudioHandoffAAD           = "new-api-node-studio:v2"
)

func EncryptNodeStudioHandoff(payload *dto.NodeStudioHandoffPayload, secret string) (string, error) {
	if payload == nil {
		return "", errors.New("Node Studio handoff payload is required")
	}
	if payload.Version != NodeStudioHandoffVersion {
		return "", fmt.Errorf("unsupported Node Studio handoff version: %d", payload.Version)
	}

	normalizedSecret := strings.TrimSpace(secret)
	if normalizedSecret == "" {
		return "", errors.New("Node Studio handoff secret is required")
	}

	plaintext, err := common.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal Node Studio handoff payload: %w", err)
	}

	key := sha256.Sum256([]byte(normalizedSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", fmt.Errorf("create Node Studio handoff cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create Node Studio handoff GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", fmt.Errorf("create Node Studio handoff nonce: %w", err)
	}

	sealed := gcm.Seal(nil, nonce, plaintext, []byte(NodeStudioHandoffAAD))
	return NodeStudioHandoffVersionPrefix + "." + base64.RawURLEncoding.EncodeToString(nonce) + "." + base64.RawURLEncoding.EncodeToString(sealed), nil
}
