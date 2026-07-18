package system_setting

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

const DefaultNodeStudioURL = "https://node.dstopology.com/auth/import-keys"

type NodeStudioSettings struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Secret  string `json:"secret"`
}

var defaultNodeStudioSettings = NodeStudioSettings{
	Enabled: false,
	URL:     DefaultNodeStudioURL,
	Secret:  "",
}

func init() {
	config.GlobalConfig.Register("node_studio", &defaultNodeStudioSettings)
}

func GetNodeStudioSettings() NodeStudioSettings {
	return defaultNodeStudioSettings
}

func ValidateNodeStudioURL(rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if strings.Contains(trimmed, "#") {
		return fmt.Errorf("Node Studio URL must not contain a fragment")
	}
	parsed, err := url.ParseRequestURI(trimmed)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("invalid Node Studio URL")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("Node Studio URL must use http or https")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("Node Studio URL must not contain credentials or a fragment")
	}
	return nil
}

func IsNodeStudioReady() bool {
	settings := GetNodeStudioSettings()
	if !settings.Enabled || strings.TrimSpace(settings.Secret) == "" {
		return false
	}
	return ValidateNodeStudioURL(settings.URL) == nil
}
