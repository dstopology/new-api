package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/stretchr/testify/require"
)

func TestCalculateTaskQuotaRespectsBillingMode(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		ratios   map[string]float64
		expected int
	}{
		{
			name:     "per request ignores seconds",
			mode:     billing_setting.BillingModePerRequest,
			ratios:   map[string]float64{"seconds": 8, "size": 1},
			expected: 750000,
		},
		{
			name:     "per second applies seconds",
			mode:     billing_setting.BillingModePerSecond,
			ratios:   map[string]float64{"seconds": 8, "size": 1},
			expected: 6000000,
		},
		{
			name:     "per request keeps non-duration multiplier",
			mode:     billing_setting.BillingModePerRequest,
			ratios:   map[string]float64{"seconds": 8, "resolution": 1.5},
			expected: 1125000,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := service.TaskBillingPolicy{Mode: test.mode}
			require.Equal(t, test.expected, calculateTaskQuota(750000, test.ratios, policy))
		})
	}
}
