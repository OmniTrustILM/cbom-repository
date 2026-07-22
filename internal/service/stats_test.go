package service_test

import (
	"context"
	"strings"
	"testing"

	"github.com/CZERTAINLY/CBOM-Repository/internal/service"

	cdx "github.com/CycloneDX/cyclonedx-go"
	"github.com/stretchr/testify/require"
)

func TestStats1(t *testing.T) {
	jsonBom := `{
  "bomFormat": "CycloneDX",
  "specVersion": "1.6",
  "serialNumber": "urn:uuid:e8c355aa-2142-4084-a8c7-6d42c8610ba2",
  "version": 1,
  "metadata": {
    "timestamp": "2024-01-09T12:00:00Z",
    "component": {
      "type": "application",
      "name": "my application",
      "version": "1.0"
    }
  },
  "components": [
    {
      "name": "RSA-2048",
      "type": "cryptographic-asset",
      "bom-ref": "crypto/key/rsa-2048@1.2.840.113549.1.1.1",
      "cryptoProperties": {
        "assetType": "related-crypto-material",
        "relatedCryptoMaterialProperties": {
          "type": "public-key",
          "id": "2e9ef09e-dfac-4526-96b4-d02f31af1b22",
          "state": "active",
          "size": 2048,
          "algorithmRef": "crypto/algorithm/rsa-2048@1.2.840.113549.1.1.1",
          "securedBy": {
            "mechanism": "Software",
            "algorithmRef": "crypto/algorithm/aes-128-gcm@2.16.840.1.101.3.4.1.6"
          },
          "creationDate": "2016-11-21T08:00:00Z",
          "activationDate": "2016-11-21T08:20:00Z"
        },
        "oid": "1.2.840.113549.1.1.1"
      }
    },
    {
      "name": "RSA-2048",
      "type": "cryptographic-asset",
      "bom-ref": "crypto/algorithm/rsa-2048@1.2.840.113549.1.1.1",
      "cryptoProperties": {
        "assetType": "algorithm",
        "algorithmProperties": {
          "parameterSetIdentifier": "2048",
          "executionEnvironment": "software-plain-ram",
          "implementationPlatform": "x86_64",
          "cryptoFunctions": [ "encapsulate", "decapsulate" ]
        },
        "oid": "1.2.840.113549.1.1.1"
      }
    },
    {
      "name": "RSA-2048",
      "type": "cryptographic-asset",
      "bom-ref": "crypto/algorithm/rsa-2048@1.5.820.122543.8.8.8"
    },
	{
      "name": "google.com",
      "type": "cryptographic-asset",
      "bom-ref": "crypto/certificate/google.com@sha256:1e15e0fbd3ce95bde5945633ae96add551341b11e5bae7bba12e98ad84a5beb4",
      "cryptoProperties": {
        "assetType": "certificate",
        "certificateProperties": {
          "subjectName": "CN = www.google.com",
          "issuerName": "C = US, O = Google Trust Services LLC, CN = GTS CA 1C3",
          "notValidBefore": "2016-11-21T08:00:00Z",
          "notValidAfter": "2017-11-22T07:59:59Z",
          "signatureAlgorithmRef": "crypto/algorithm/sha-512-rsa@1.2.840.113549.1.1.13",
          "subjectPublicKeyRef": "crypto/key/rsa-2048@1.2.840.113549.1.1.1",
          "certificateFormat": "X.509",
          "certificateExtension": "crt"
        }
      }
    },
	{
      "name": "made-up-protocol",
      "type": "cryptographic-asset",
      "bom-ref": "i've made up this too",
      "cryptoProperties": {
        "assetType": "protocol",
        "protocolProperties": {
			"type" : "tls",
			"Version" : "v1"
        }
      }
    },
    {
      "name": "RSA",
      "type": "framework",
      "bom-ref": "i-made-this-up"
    },
    {
      "name": "AES-128-GCM",
      "type": "cryptographic-asset",
      "bom-ref": "crypto/algorithm/aes-128-gcm@2.16.840.1.101.3.4.1.6",
      "cryptoProperties": {
        "assetType": "algorithm",
        "algorithmProperties": {
          "parameterSetIdentifier": "128",
          "primitive": "ae",
          "mode": "gcm",
          "executionEnvironment": "software-plain-ram",
          "implementationPlatform": "x86_64",
          "cryptoFunctions": [ "keygen", "encrypt", "decrypt" ],
          "classicalSecurityLevel": 128,
          "nistQuantumSecurityLevel": 1
        },
        "oid": "2.16.840.1.101.3.4.1.6"
      }
    }
  ]
}`
	var bom cdx.BOM
	decoder := cdx.NewBOMDecoder(strings.NewReader(jsonBom), cdx.BOMFileFormatJSON)
	err := decoder.Decode(&bom)
	require.NoError(t, err)

	bomStats := service.CalculateCryptoStats(context.Background(), &bom)

	require.Equal(t, 5, bomStats.CryptoAsset.Total)
	require.Equal(t, 2, bomStats.CryptoAsset.Algo.Total)
	require.Equal(t, 1, bomStats.CryptoAsset.Cert.Total)
	require.Equal(t, 1, bomStats.CryptoAsset.Protocol.Total)
	require.Equal(t, 1, bomStats.CryptoAsset.Related.Total)
}

func TestStatsComponentsNil(t *testing.T) {
	jsonBom := `{
  "bomFormat": "CycloneDX",
  "specVersion": "1.6",
  "serialNumber": "urn:uuid:e8c355aa-2142-4084-a8c7-6d42c8610ba2",
  "version": 1,
  "metadata": {
    "timestamp": "2024-01-09T12:00:00Z",
    "component": {
      "type": "application",
      "name": "my application",
      "version": "1.0"
    }
  }
}`
	var bom cdx.BOM
	decoder := cdx.NewBOMDecoder(strings.NewReader(jsonBom), cdx.BOMFileFormatJSON)
	err := decoder.Decode(&bom)
	require.NoError(t, err)

	bomStats := service.CalculateCryptoStats(context.Background(), &bom)

	require.Equal(t, 0, bomStats.CryptoAsset.Total)
	require.Equal(t, 0, bomStats.CryptoAsset.Algo.Total)
	require.Equal(t, 0, bomStats.CryptoAsset.Cert.Total)
	require.Equal(t, 0, bomStats.CryptoAsset.Protocol.Total)
	require.Equal(t, 0, bomStats.CryptoAsset.Related.Total)

}

func TestCalculateCryptoStats_1_7_ParityWithBuckets(t *testing.T) {
	input := `{
  "bomFormat": "CycloneDX",
  "specVersion": "1.7",
  "components": [
    { "type": "cryptographic-asset", "name": "a", "cryptoProperties": { "assetType": "algorithm" } },
    { "type": "cryptographic-asset", "name": "c", "cryptoProperties": { "assetType": "certificate" } },
    { "type": "cryptographic-asset", "name": "p", "cryptoProperties": { "assetType": "protocol", "protocolProperties": { "type": "quic" } } },
    { "type": "cryptographic-asset", "name": "r", "cryptoProperties": { "assetType": "related-crypto-material" } }
  ]
}`
	var bom cdx.BOM
	require.NoError(t, cdx.NewBOMDecoder(strings.NewReader(input), cdx.BOMFileFormatJSON).Decode(&bom))

	stats := service.CalculateCryptoStats(context.Background(), &bom)
	require.Equal(t, 4, stats.CryptoAsset.Total)
	require.Equal(t, 1, stats.CryptoAsset.Algo.Total)
	require.Equal(t, 1, stats.CryptoAsset.Cert.Total)
	require.Equal(t, 1, stats.CryptoAsset.Protocol.Total)
	require.Equal(t, 1, stats.CryptoAsset.Related.Total)
}
