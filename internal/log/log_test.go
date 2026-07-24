package log_test

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/OmniTrustILM/cbom-repository/internal/log"
	"github.com/stretchr/testify/require"
)

func TestContextAttrs(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		given []slog.Attr
		then  string
	}{
		"nil attributes": {
			given: nil,
			then:  `{"level":"WARN","msg":"this is a test message","condition":"exceeded"}`,
		},
		"empty attributes": {
			given: []slog.Attr{},
			then:  `{"level":"WARN","msg":"this is a test message","condition":"exceeded"}`,
		},
		"actual attributes": {
			given: []slog.Attr{
				slog.String("error", "not found"),
			},
			then: `{"level":"WARN","msg":"this is a test message","condition":"exceeded","error":"not found"}`,
		},
		"slog group": {
			given: []slog.Attr{
				slog.Group("group", slog.String("error", "not found")),
			},
			then: `{"level":"WARN","msg":"this is a test message","condition":"exceeded", "group": {"error":"not found"}}`,
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
				AddSource: false,
				Level:     slog.LevelDebug,
				ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
					if a.Key == slog.TimeKey {
						return slog.Attr{}
					}
					return a
				},
			})
			ctxHandler := log.New(base)
			logger := slog.New(ctxHandler)

			ctx := log.ContextAttrs(t.Context(), tt.given...)
			logger.WarnContext(ctx, "this is a test message", slog.String("condition", "exceeded"))

			t.Logf("log output: %s", buf.String())
			require.JSONEq(t, tt.then, buf.String())
		})
	}
}
