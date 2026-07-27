package http

import (
	"context"
	"testing"

	"github.com/OmniTrustILM/cbom-repository/internal/health"
	"github.com/OmniTrustILM/cbom-repository/internal/service"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name           string
		cfg            Config
		expectedPrefix string
	}{
		{
			name: "default prefix",
			cfg: Config{
				Port:   8080,
				Prefix: "/api",
			},
			expectedPrefix: "/api",
		},
		{
			name: "prefix with trailing slash",
			cfg: Config{
				Port:   8080,
				Prefix: "/v1/",
			},
			expectedPrefix: "/v1",
		},
		{
			name: "prefix without leading slash",
			cfg: Config{
				Port:   8080,
				Prefix: "api",
			},
			expectedPrefix: "/api",
		},
		{
			name: "empty prefix",
			cfg: Config{
				Port:   8080,
				Prefix: "",
			},
			expectedPrefix: "",
		},
		{
			name: "custom prefix",
			cfg: Config{
				Port:   9090,
				Prefix: "/custom/path",
			},
			expectedPrefix: "/custom/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storageChecker := mockChecker{name: "storage", status: health.StatusUp, details: map[string]any{"latencyMs": 1}}
			healthSvc := health.NewService(storageChecker)

			server := New(tt.cfg, service.Service{}, healthSvc)

			require.Equal(t, tt.expectedPrefix, server.cfg.Prefix)
			require.Equal(t, tt.cfg.Port, server.cfg.Port)
		})
	}
}

func TestHandler(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
	}{
		{
			name:   "default prefix",
			prefix: "/api",
		},
		{
			name:   "custom prefix",
			prefix: "/v2",
		},
		{
			name:   "empty prefix",
			prefix: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Port:   8080,
				Prefix: tt.prefix,
			}

			storageChecker := mockChecker{name: "storage", status: health.StatusUp, details: map[string]any{"latencyMs": 1}}
			healthSvc := health.NewService(storageChecker)

			server := New(cfg, service.Service{}, healthSvc)
			router := server.Handler()

			require.NotNil(t, router)
			require.NotNil(t, router.NotFoundHandler)
		})
	}
}

// mockChecker is a mock implementation of the health.Checker interface used by health.NewService
type mockChecker struct {
	name    string
	status  health.Status
	details map[string]any
}

func (m mockChecker) Name() string { return m.name }
func (m mockChecker) Check(ctx context.Context) health.Component {
	return health.Component{Status: m.status, Details: m.details}
}
