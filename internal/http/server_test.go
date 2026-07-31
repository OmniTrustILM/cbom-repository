package http

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestCORSHeadersOnSimpleRequest(t *testing.T) {
	tests := []struct {
		name            string
		allowedOrigins  []string
		requestOrigin   string
		wantAllowOrigin string
		wantVary        string
	}{
		{
			name:            "cors disabled emits no headers",
			allowedOrigins:  nil,
			requestOrigin:   "http://localhost:8000",
			wantAllowOrigin: "",
			wantVary:        "",
		},
		{
			name:            "allowed origin is echoed",
			allowedOrigins:  []string{"http://localhost:8000"},
			requestOrigin:   "http://localhost:8000",
			wantAllowOrigin: "http://localhost:8000",
			wantVary:        "Origin",
		},
		{
			name:            "unlisted origin gets vary but no allow header",
			allowedOrigins:  []string{"http://localhost:8000"},
			requestOrigin:   "http://attacker.example",
			wantAllowOrigin: "",
			wantVary:        "Origin",
		},
		{
			name:            "wildcard echoes the request origin",
			allowedOrigins:  []string{"*"},
			requestOrigin:   "http://localhost:8000",
			wantAllowOrigin: "http://localhost:8000",
			wantVary:        "Origin",
		},
		{
			name:            "origin matching is case-insensitive",
			allowedOrigins:  []string{"http://LocalHost:8000"},
			requestOrigin:   "http://localhost:8000",
			wantAllowOrigin: "http://localhost:8000",
			wantVary:        "Origin",
		},
		{
			name:            "same-origin request without an origin header is untouched",
			allowedOrigins:  []string{"http://localhost:8000"},
			requestOrigin:   "",
			wantAllowOrigin: "",
			wantVary:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				Port:               8080,
				Prefix:             "/api",
				MaxBodySize:        20971520,
				CORSAllowedOrigins: tt.allowedOrigins,
			}
			storageChecker := mockChecker{name: "storage", status: health.StatusUp, details: map[string]any{"latencyMs": 1}}
			server := New(cfg, service.Service{}, health.NewService(storageChecker))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
			if tt.requestOrigin != "" {
				req.Header.Set("Origin", tt.requestOrigin)
			}
			rec := httptest.NewRecorder()
			server.Handler().ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, tt.wantAllowOrigin, rec.Header().Get("Access-Control-Allow-Origin"))
			require.Equal(t, tt.wantVary, rec.Header().Get("Vary"))
		})
	}
}
