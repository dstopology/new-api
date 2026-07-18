package sora

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"golang.org/x/image/webp"
)

const (
	maxFireflyPromptRunes = 1200
	maxFireflyImageBytes  = 10 * 1024 * 1024
)

type fireflyVideoFields struct {
	AspectRatio    string  `json:"aspect_ratio,omitempty"`
	Duration       *int    `json:"duration,omitempty"`
	GenerateAudio  *bool   `json:"generate_audio,omitempty"`
	NegativePrompt string  `json:"negative_prompt,omitempty"`
	ReferenceMode  string  `json:"reference_mode,omitempty"`
	Resolution     string  `json:"resolution,omitempty"`
	Seconds        *string `json:"seconds,omitempty"`
}

type fireflyVideoProfile struct {
	maxImages      int
	referenceMode  string
	durations      []int
	negativePrompt bool
	resolution     bool
	disableAudio   bool
}

func fireflyProfile(modelName string) (fireflyVideoProfile, bool) {
	switch strings.ToLower(strings.TrimSpace(modelName)) {
	case "sora-2", "sora-2-pro":
		return fireflyVideoProfile{
			maxImages:      1,
			referenceMode:  "frame",
			durations:      []int{4, 8, 12},
			negativePrompt: true,
		}, true
	case "veo-3-1", "veo-3-1-fast":
		return fireflyVideoProfile{
			maxImages:     2,
			referenceMode: "frame",
			durations:     []int{4, 6, 8},
			resolution:    true,
			disableAudio:  true,
		}, true
	case "veo-3-1-ref":
		// The live upstream currently rejects 4s and 6s after accepting the
		// asynchronous task, despite advertising all three values.
		return fireflyVideoProfile{
			maxImages:     3,
			referenceMode: "image",
			durations:     []int{8},
			resolution:    true,
			disableAudio:  true,
		}, true
	default:
		return fireflyVideoProfile{}, false
	}
}

func validateFireflyVideoRequest(modelName string, request relaycommon.TaskSubmitReq, fields fireflyVideoFields, jsonRequest bool) error {
	profile, ok := fireflyProfile(modelName)
	if !ok {
		return nil
	}

	if utf8.RuneCountInString(request.Prompt) > maxFireflyPromptRunes {
		return fmt.Errorf("prompt must not exceed %d characters for model %s", maxFireflyPromptRunes, modelName)
	}
	if jsonRequest && (strings.TrimSpace(request.Image) != "" || strings.TrimSpace(request.InputReference) != "") {
		return fmt.Errorf("model %s requires JSON reference images in the images array", modelName)
	}
	if len(request.Images) > profile.maxImages {
		return fmt.Errorf("model %s accepts at most %d reference image(s)", modelName, profile.maxImages)
	}
	for index, image := range request.Images {
		if err := validateFireflyImageSource(image); err != nil {
			return fmt.Errorf("images[%d]: %w", index, err)
		}
	}

	duration, supplied, err := fireflyDuration(request, fields, jsonRequest)
	if err != nil {
		return err
	}
	if supplied && !containsInt(profile.durations, duration) {
		return fmt.Errorf("model %s only supports duration values %v", modelName, profile.durations)
	}

	if fields.AspectRatio != "" && fields.AspectRatio != "16:9" && fields.AspectRatio != "9:16" {
		return fmt.Errorf("model %s only supports aspect_ratio 16:9 or 9:16", modelName)
	}
	if fields.ReferenceMode != "" && fields.ReferenceMode != profile.referenceMode {
		return fmt.Errorf("model %s requires reference_mode %q", modelName, profile.referenceMode)
	}
	if utf8.RuneCountInString(fields.NegativePrompt) > maxFireflyPromptRunes {
		return fmt.Errorf("negative_prompt must not exceed %d characters for model %s", maxFireflyPromptRunes, modelName)
	}
	if strings.TrimSpace(fields.NegativePrompt) != "" && !profile.negativePrompt {
		return fmt.Errorf("model %s does not support negative_prompt", modelName)
	}
	if fields.Resolution != "" {
		if !profile.resolution {
			return fmt.Errorf("model %s does not support resolution", modelName)
		}
		if fields.Resolution != "720p" && fields.Resolution != "1080p" {
			return fmt.Errorf("model %s only supports resolution 720p or 1080p", modelName)
		}
	}
	if fields.GenerateAudio != nil && !*fields.GenerateAudio && !profile.disableAudio {
		return fmt.Errorf("model %s cannot disable generated audio on the current upstream", modelName)
	}
	return nil
}

func normalizeFireflyVideoBody(body map[string]interface{}, modelName string) {
	profile, ok := fireflyProfile(modelName)
	if !ok {
		return
	}
	if _, hasImages := body["images"]; hasImages {
		body["reference_mode"] = profile.referenceMode
	}
}

func fireflyDuration(request relaycommon.TaskSubmitReq, fields fireflyVideoFields, jsonRequest bool) (int, bool, error) {
	if jsonRequest && (fields.Duration != nil || fields.Seconds != nil) {
		if fields.Duration != nil && fields.Seconds != nil {
			return 0, false, fmt.Errorf("duration and seconds must not be provided together")
		}
		if fields.Duration != nil {
			return *fields.Duration, true, nil
		}
		value, err := strconv.Atoi(strings.TrimSpace(*fields.Seconds))
		if err != nil {
			return 0, false, fmt.Errorf("seconds must be an integer")
		}
		return value, true, nil
	}
	seconds := strings.TrimSpace(request.Seconds)
	if seconds != "" && request.Duration != 0 {
		return 0, false, fmt.Errorf("duration and seconds must not be provided together")
	}
	if seconds != "" {
		value, err := strconv.Atoi(seconds)
		if err != nil {
			return 0, false, fmt.Errorf("seconds must be an integer")
		}
		return value, true, nil
	}
	if request.Duration != 0 {
		return request.Duration, true, nil
	}
	return 0, false, nil
}

func validateFireflyImageSource(source string) error {
	source = strings.TrimSpace(source)
	if source == "" {
		return fmt.Errorf("reference image must not be empty")
	}
	if len(source) >= len("data:") && strings.EqualFold(source[:len("data:")], "data:") {
		comma := strings.IndexByte(source, ',')
		if comma < 0 {
			return fmt.Errorf("invalid data URI")
		}
		header := strings.ToLower(source[:comma])
		parts := strings.Split(header, ";")
		mimeType := strings.TrimPrefix(parts[0], "data:")
		if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" {
			return fmt.Errorf("data URI must contain a JPEG, PNG, or WebP image")
		}
		base64Encoded := false
		for _, part := range parts[1:] {
			if part == "base64" {
				base64Encoded = true
				break
			}
		}
		if !base64Encoded {
			return fmt.Errorf("image data URI must use base64 encoding")
		}
		payload := source[comma+1:]
		if base64.StdEncoding.DecodedLen(len(payload)) > maxFireflyImageBytes+2 {
			return fmt.Errorf("reference image must not exceed 10 MB")
		}
		decoded, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return fmt.Errorf("invalid base64 image data")
		}
		if len(decoded) > maxFireflyImageBytes {
			return fmt.Errorf("reference image must not exceed 10 MB")
		}
		if err := validateFireflyImageData(mimeType, decoded); err != nil {
			return err
		}
		return nil
	}

	parsed, err := url.Parse(source)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("reference image must be an HTTP(S) URL or image data URI")
	}
	return nil
}

func validateFireflyImageData(mimeType string, data []byte) error {
	if mimeType == "image/webp" {
		if _, err := webp.DecodeConfig(bytes.NewReader(data)); err != nil {
			return fmt.Errorf("invalid WebP image data")
		}
		return nil
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("invalid image data")
	}
	expectedFormat := "png"
	if mimeType == "image/jpeg" {
		expectedFormat = "jpeg"
	}
	if format != expectedFormat {
		return fmt.Errorf("image data does not match declared MIME type")
	}
	return nil
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
