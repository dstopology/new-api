package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/stretchr/testify/require"
)

func TestGetSyncablePricingBillingMode(t *testing.T) {
	tests := []struct {
		name      string
		quotaType int
		mode      string
		expr      string
		wantMode  string
		wantExpr  string
		wantOK    bool
	}{
		{
			name:      "per second fixed price",
			quotaType: 1,
			mode:      billing_setting.BillingModePerSecond,
			wantMode:  billing_setting.BillingModePerSecond,
			wantOK:    true,
		},
		{
			name:      "per request fixed price",
			quotaType: 1,
			mode:      billing_setting.BillingModePerRequest,
			wantMode:  billing_setting.BillingModePerRequest,
			wantOK:    true,
		},
		{
			name:      "tiered expression",
			quotaType: 0,
			mode:      billing_setting.BillingModeTieredExpr,
			expr:      `tier("base", p * 1 + c * 2)`,
			wantMode:  billing_setting.BillingModeTieredExpr,
			wantExpr:  `tier("base", p * 1 + c * 2)`,
			wantOK:    true,
		},
		{
			name:      "per second requires fixed price quota type",
			quotaType: 0,
			mode:      billing_setting.BillingModePerSecond,
		},
		{
			name:      "tiered expression requires expression",
			quotaType: 1,
			mode:      billing_setting.BillingModeTieredExpr,
			expr:      "   ",
		},
		{
			name:      "unknown mode",
			quotaType: 1,
			mode:      "per_minute",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mode, expr, ok := getSyncablePricingBillingMode(test.quotaType, test.mode, test.expr)
			require.Equal(t, test.wantOK, ok)
			require.Equal(t, test.wantMode, mode)
			require.Equal(t, test.wantExpr, expr)
		})
	}
}
