package http_test

import (
	"testing"

	internalHttp "github.com/CZERTAINLY/CBOM-Repository/internal/http"
	"github.com/stretchr/testify/require"
)

func TestUploadInputChecks(t *testing.T) {
	testCases := map[string]struct {
		input          string
		wantErr        bool
		version        string
		defaultVersion string
	}{
		"empty": {
			input:   "",
			wantErr: true,
		},
		"multiple": {
			input:   "application/json, text/plain",
			wantErr: true,
		},
		"missing version defaults to 1.6": {
			input:          "application/vnd.cyclonedx+json",
			wantErr:        false,
			version:        "1.6",
			defaultVersion: "1.6",
		},
		"missing version honours configured default 1.7": {
			input:          "application/vnd.cyclonedx+json",
			wantErr:        false,
			version:        "1.7",
			defaultVersion: "1.7",
		},
		"expected content type": {
			input:          "application/vnd.cyclonedx+json; Version = 1.4",
			wantErr:        false,
			version:        "1.4",
			defaultVersion: "1.6",
		},
		"unexpected-1": {
			input:   "application/json",
			wantErr: true,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ok, version := internalHttp.CheckContentType(tc.input, tc.defaultVersion)
			if tc.wantErr {
				require.False(t, ok)
			} else {
				require.True(t, ok)
				require.Equal(t, tc.version, version)
			}
		})
	}
}
