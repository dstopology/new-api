package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitDefaultVendorMappingFillsMissingVideoVendors(t *testing.T) {
	metaMap := map[string]*Model{
		"sora-2":       {ModelName: "sora-2"},
		"veo-3-1-fast": {ModelName: "veo-3-1-fast"},
	}
	vendorMap := map[int]*Vendor{
		1: {Id: 1, Name: "OpenAI", Icon: "OpenAI"},
		2: {Id: 2, Name: "Google", Icon: "Gemini.Color"},
	}
	abilities := []AbilityWithChannel{
		{Ability: Ability{Model: "sora-2"}},
		{Ability: Ability{Model: "veo-3-1-fast"}},
	}

	initDefaultVendorMapping(metaMap, vendorMap, abilities)

	require.Equal(t, 1, metaMap["sora-2"].VendorID)
	require.Equal(t, 2, metaMap["veo-3-1-fast"].VendorID)
}
