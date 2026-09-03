package store_test

import (
	"testing"

	"github.com/OmniTrustILM/cbom-repository/internal/store"

	"github.com/stretchr/testify/require"
)

func TestMetadata_Map(t *testing.T) {
	t.Run("with stats version", func(t *testing.T) {
		m := store.Metadata{Version: "1", CryptoStats: "{}", CryptoStatsVersion: "2"}
		require.Equal(t, map[string]string{
			store.MetaVersionKey:            "1",
			store.MetaCryptoStatsKey:        "{}",
			store.MetaCryptoStatsVersionKey: "2",
		}, m.Map())
	})

	t.Run("without stats version the key is omitted", func(t *testing.T) {
		m := store.Metadata{Version: "original", CryptoStats: "{}"}
		require.Equal(t, map[string]string{
			store.MetaVersionKey:     "original",
			store.MetaCryptoStatsKey: "{}",
		}, m.Map())
	})
}
