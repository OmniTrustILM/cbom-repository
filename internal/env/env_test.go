package env_test

import (
	"log/slog"
	"testing"

	"github.com/CZERTAINLY/CBOM-Repository/internal/env"
	"github.com/CZERTAINLY/CBOM-Repository/internal/http"
	"github.com/CZERTAINLY/CBOM-Repository/internal/service"
	"github.com/CZERTAINLY/CBOM-Repository/internal/store"

	"github.com/stretchr/testify/require"
)

func TestNewFunc(t *testing.T) {
	testCases := map[string]struct {
		envVars map[string]string
		wantErr bool
		want    env.Config
	}{
		"success": {
			envVars: map[string]string{
				"APP_S3_REGION":          "eu-west-1",
				"APP_S3_ENDPOINT":        "http://localhost:9000",
				"APP_S3_BUCKET":          "czertainly",
				"APP_S3_ACCESS_KEY":      "minioadmin",
				"APP_S3_SECRET_KEY":      "adminpassword",
				"APP_S3_USE_PATH_STYLE":  "true",
				"APP_HTTP_PORT":          "8090",
				"APP_HTTP_PREFIX":        "/cbom/repo",
				"APP_HTTP_MAX_BODY_SIZE": "512",
				"APP_LOG_LEVEL":          "DEBUG",
				"APP_CHECK_ON_FETCH":     "true",
			},
			wantErr: false,
			want: env.Config{
				Store: store.Config{
					Region:       "eu-west-1",
					Endpoint:     "http://localhost:9000",
					Bucket:       "czertainly",
					AccessKey:    "minioadmin",
					SecretKey:    "adminpassword",
					UsePathStyle: true,
				},
				Http: http.Config{
					Port:              8090,
					Prefix:            "/cbom/repo",
					MaxBodySize:       512,
					DefaultBOMVersion: "1.6",
				},
				LogLevel: slog.LevelDebug,
				Service: service.Config{
					CheckOnFetch: true,
				},
			},
		},
		"log level, checkOnFetch, http port, prefix and max body size have default value": {
			envVars: map[string]string{
				"APP_S3_REGION":         "eu-west-1",
				"APP_S3_ENDPOINT":       "http://localhost:9000",
				"APP_S3_BUCKET":         "czertainly",
				"APP_S3_ACCESS_KEY":     "minioadmin",
				"APP_S3_SECRET_KEY":     "adminpassword",
				"APP_S3_USE_PATH_STYLE": "true",
			},
			wantErr: false,
			want: env.Config{
				Store: store.Config{
					Region:       "eu-west-1",
					Endpoint:     "http://localhost:9000",
					Bucket:       "czertainly",
					AccessKey:    "minioadmin",
					SecretKey:    "adminpassword",
					UsePathStyle: true,
				},
				Http: http.Config{
					Port:              8080,
					Prefix:            "/api",
					MaxBodySize:       20971520,
					DefaultBOMVersion: "1.6",
				},
				LogLevel: slog.LevelInfo,
				Service: service.Config{
					CheckOnFetch: false,
				},
			},
		},
		"port must be a number": {
			envVars: map[string]string{
				"APP_S3_REGION":         "eu-west-1",
				"APP_S3_ENDPOINT":       "http://localhost:9000",
				"APP_S3_BUCKET":         "czertainly",
				"APP_S3_ACCESS_KEY":     "minioadmin",
				"APP_S3_SECRET_KEY":     "adminpassword",
				"APP_S3_USE_PATH_STYLE": "true",
				"APP_HTTP_PORT":         "eighty",
			},
			wantErr: true,
		},
		"http max body size must be a number": {
			envVars: map[string]string{
				"APP_S3_REGION":          "eu-west-1",
				"APP_S3_ENDPOINT":        "http://localhost:9000",
				"APP_S3_BUCKET":          "czertainly",
				"APP_S3_ACCESS_KEY":      "minioadmin",
				"APP_S3_SECRET_KEY":      "adminpassword",
				"APP_S3_USE_PATH_STYLE":  "true",
				"APP_HTTP_MAX_BODY_SIZE": "eighty",
			},
			wantErr: true,
		},
		"http max body size must not be zero": {
			envVars: map[string]string{
				"APP_S3_REGION":          "eu-west-1",
				"APP_S3_ENDPOINT":        "http://localhost:9000",
				"APP_S3_BUCKET":          "czertainly",
				"APP_S3_ACCESS_KEY":      "minioadmin",
				"APP_S3_SECRET_KEY":      "adminpassword",
				"APP_S3_USE_PATH_STYLE":  "true",
				"APP_HTTP_MAX_BODY_SIZE": "0",
			},
			wantErr: true,
		},
		"http max body size must be a positive integer": {
			envVars: map[string]string{
				"APP_S3_REGION":          "eu-west-1",
				"APP_S3_ENDPOINT":        "http://localhost:9000",
				"APP_S3_BUCKET":          "czertainly",
				"APP_S3_ACCESS_KEY":      "minioadmin",
				"APP_S3_SECRET_KEY":      "adminpassword",
				"APP_S3_USE_PATH_STYLE":  "true",
				"APP_HTTP_MAX_BODY_SIZE": "-20",
			},
			wantErr: true,
		},
		"path style can be false": {
			envVars: map[string]string{
				"APP_S3_REGION":         "eu-west-1",
				"APP_S3_ENDPOINT":       "http://localhost:9000",
				"APP_S3_BUCKET":         "czertainly",
				"APP_S3_ACCESS_KEY":     "minioadmin",
				"APP_S3_SECRET_KEY":     "adminpassword",
				"APP_S3_USE_PATH_STYLE": "false",
			},
			wantErr: false,
			want: env.Config{
				Store: store.Config{
					Region:       "eu-west-1",
					Endpoint:     "http://localhost:9000",
					Bucket:       "czertainly",
					AccessKey:    "minioadmin",
					SecretKey:    "adminpassword",
					UsePathStyle: false,
				},
				Http: http.Config{
					Port:              8080,
					Prefix:            "/api",
					MaxBodySize:       20971520,
					DefaultBOMVersion: "1.6",
				},
				LogLevel: slog.LevelInfo,
				Service: service.Config{
					CheckOnFetch: false,
				},
			},
		},
		"path style has a default value": {
			envVars: map[string]string{
				"APP_S3_REGION":     "eu-west-1",
				"APP_S3_ENDPOINT":   "http://localhost:9000",
				"APP_S3_BUCKET":     "czertainly",
				"APP_S3_ACCESS_KEY": "minioadmin",
				"APP_S3_SECRET_KEY": "adminpassword",
			},
			wantErr: false,
			want: env.Config{
				Store: store.Config{
					Region:       "eu-west-1",
					Endpoint:     "http://localhost:9000",
					Bucket:       "czertainly",
					AccessKey:    "minioadmin",
					SecretKey:    "adminpassword",
					UsePathStyle: true,
				},
				Http: http.Config{
					Port:              8080,
					Prefix:            "/api",
					MaxBodySize:       20971520,
					DefaultBOMVersion: "1.6",
				},
				LogLevel: slog.LevelInfo,
				Service: service.Config{
					CheckOnFetch: false,
				},
			},
		},
		"endpoint may be omitted": {
			envVars: map[string]string{
				"APP_S3_REGION":         "eu-west-1",
				"APP_S3_BUCKET":         "czertainly",
				"APP_S3_ACCESS_KEY":     "minioadmin",
				"APP_S3_SECRET_KEY":     "adminpassword",
				"APP_S3_USE_PATH_STYLE": "true",
			},
			wantErr: false,
			want: env.Config{
				Store: store.Config{
					Region:       "eu-west-1",
					Bucket:       "czertainly",
					AccessKey:    "minioadmin",
					SecretKey:    "adminpassword",
					UsePathStyle: true,
				},
				Http: http.Config{
					Port:              8080,
					Prefix:            "/api",
					MaxBodySize:       20971520,
					DefaultBOMVersion: "1.6",
				},
				LogLevel: slog.LevelInfo,
				Service: service.Config{
					CheckOnFetch: false,
				},
			},
		},
		"whitespaces-only-bucket": {
			envVars: map[string]string{
				"APP_S3_REGION":     "eu-west-1",
				"APP_S3_BUCKET":     " \t\r \n  ",
				"APP_S3_ACCESS_KEY": "minioadmin",
				"APP_S3_SECRET_KEY": "adminpassword",
			},
			wantErr: true,
		},
		"whitespaces-only-aws-region": {
			envVars: map[string]string{
				"APP_S3_REGION":     "  \t \t  ",
				"APP_S3_BUCKET":     "czertainly",
				"APP_S3_ACCESS_KEY": "minioadmin",
				"APP_S3_SECRET_KEY": "adminpassword",
			},
			wantErr: true,
		},
		"whitespaces-only-access-key": {
			envVars: map[string]string{
				"APP_S3_REGION":     "eu-west-1",
				"APP_S3_BUCKET":     "czertainly",
				"APP_S3_ACCESS_KEY": "      ",
				"APP_S3_SECRET_KEY": "adminpassword",
			},
			wantErr: true,
		},
		"whitespaces-only-aws-secret": {
			envVars: map[string]string{
				"APP_S3_REGION":     "eu-west-1",
				"APP_S3_BUCKET":     "czertainly",
				"APP_S3_ACCESS_KEY": "minioadmin",
				"APP_S3_SECRET_KEY": " \t  \t",
			},
			wantErr: true,
		},
		"bucket-missing": {
			envVars: map[string]string{
				"APP_S3_REGION":         "eu-west-1",
				"APP_S3_ENDPOINT":       "http://localhost:9000",
				"APP_S3_ACCESS_KEY":     "minioadmin",
				"APP_S3_SECRET_KEY":     "adminpassword",
				"APP_S3_USE_PATH_STYLE": "true",
			},
			wantErr: true,
		},
		"region-missing": {
			envVars: map[string]string{
				"APP_S3_ENDPOINT":       "http://localhost:9000",
				"APP_S3_BUCKET":         "czertainly",
				"APP_S3_ACCESS_KEY":     "minioadmin",
				"APP_S3_SECRET_KEY":     "adminpassword",
				"APP_S3_USE_PATH_STYLE": "true",
			},
			wantErr: true,
		},
		"access-key-missing": {
			envVars: map[string]string{
				"APP_S3_REGION":         "eu-west-1",
				"APP_S3_ENDPOINT":       "http://localhost:9000",
				"APP_S3_BUCKET":         "czertainly",
				"APP_S3_SECRET_KEY":     "adminpassword",
				"APP_S3_USE_PATH_STYLE": "true",
			},
			wantErr: true,
		},
		"secret-missing": {
			envVars: map[string]string{
				"APP_S3_REGION":         "eu-west-1",
				"APP_S3_ENDPOINT":       "http://localhost:9000",
				"APP_S3_BUCKET":         "czertainly",
				"APP_S3_ACCESS_KEY":     "minioadmin",
				"APP_S3_USE_PATH_STYLE": "true",
			},
			wantErr: true,
		},
		"empty environment": {
			envVars: map[string]string{},
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			setTestEnv(t, tc.envVars)

			cfg, err := env.New()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, cfg)
			}
		})
	}
}

// using `testing.Setenv()` we can prepare environment for each test case
// and have it automatically cleaned up after test
func setTestEnv(t *testing.T, envVars map[string]string) {
	t.Helper()

	for name, value := range envVars {
		t.Setenv(name, value)
	}
}
