package service

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/OmniTrustILM/cbom-repository/internal/log"
	"github.com/OmniTrustILM/cbom-repository/internal/store"

	"github.com/google/uuid"
	jss "github.com/kaptinlin/jsonschema"
)

var (
	ErrValidation    = errors.New("validation failed")
	ErrAlreadyExists = errors.New("already exists")
	ErrNotFound      = errors.New("not found")
)

//go:embed schemas
var schemas embed.FS

// Please note: When you want to add a new schema version, please first
// add the schema file into the `schemas` subdirectory in `internal/service`
// and then extend this variable with the mapping.
var versionToEmbeddedFileMapping = map[string]string{
	"1.6": "schemas/bom-1.6.schema.json",
	"1.7": "schemas/bom-1.7.schema.json",
}

// subSchemaFiles are the CycloneDX sub-schemas referenced by the bom-*.schema.json
// documents through relative `$ref`s (resolved against their `$id` base
// http://cyclonedx.org/schema/...). They are vendored here and pre-registered with
// the compiler so validation stays fully self-contained. If any of these were
// missing, the compiler would try to fetch them from cyclonedx.org over the network;
// New disables that (see noNetworkSchemaLoader) so a missing sub-schema fails loudly
// instead of being silently downloaded — a download would make validation depend on
// network reachability and, when unavailable, degrade to fail-open, accepting BOMs
// that violate these sub-schemas (e.g. the 1.7 `algorithmFamily` and `ellipticCurves`
// enums, SPDX license expressions, JSF signatures).
//
// Vendored from http://cyclonedx.org/schema/<file> (CycloneDX schemas, Apache-2.0).
var subSchemaFiles = []string{
	"schemas/spdx.schema.json",
	"schemas/jsf-0.82.schema.json",
	"schemas/cryptography-defs.schema.json",
}

// noNetworkSchemaLoader replaces the jsonschema compiler's default HTTP/HTTPS
// loaders. Every CycloneDX sub-schema is vendored and pre-registered (see
// subSchemaFiles), so external `$ref`s resolve from the compiler's cache and no
// network fetch should ever be needed. If the compiler still reaches for a loader it
// means a referenced schema is missing from the vendored set — so we log and return
// an error instead of silently downloading it from cyclonedx.org. That keeps
// validation independent of network reachability and prevents a fetched-over-the-wire
// schema from masking a missing vendored file.
func noNetworkSchemaLoader(url string) (io.ReadCloser, error) {
	err := fmt.Errorf("refused to fetch schema %q over the network: all CycloneDX sub-schemas must be vendored in internal/service/schemas and listed in subSchemaFiles", url)
	slog.Error("blocked network schema fetch during schema compilation", slog.String("url", url), slog.String("error", err.Error()))
	return nil, err
}

type Config struct {
	// CheckOnFetch controls whether service performs an unmarshal attempt on the array
	// of bytes received from backend storage (minio/s3) for get operation.
	CheckOnFetch bool `envconfig:"APP_CHECK_ON_FETCH" default:"false"`
}

type Service struct {
	config      Config
	store       store.Store
	jsonSchemas map[string]*jss.Schema
}

// New creates and initializes a new Service instance with the provided store.
// During initialization, it loads and compiles JSON schemas for all supported
// CycloneDX BOM versions from defined embedded schema files.
//
// The function reads schema files from the embedded filesystem and compiles them
// into validators that will be used to validate uploaded BOMs. If any schema file
// cannot be read or compiled, the function returns an error and the Service will
// not be initialized.
//
// Supported schema versions are defined in the versionToEmbeddedFileMapping variable.
// To add support for a new CycloneDX version, place the schema file in the schemas
// subdirectory and update the mapping.
//
// Parameters:
//   - store: The storage backend used for persisting and retrieving BOM documents
//
// Returns:
//   - Service: An initialized service ready to handle BOM operations
//   - error: Non-nil if any schema file cannot be read or compiled, nil otherwise
func New(store store.Store, config Config) (Service, error) {

	// Use a single compiler for all versions: the vendored sub-schemas are large
	// (spdx.schema.json in particular), so compile and register them once under
	// their `$id`, then resolve every bom schema's external `$ref`s from that
	// cache — instead of re-compiling the sub-schemas per version. The bom schemas
	// have distinct `$id`s, so sharing the compiler does not conflate versions.
	compiler := jss.NewCompiler()

	// Disable network schema resolution: swap the compiler's default HTTP/HTTPS
	// loaders for one that errors out (see noNetworkSchemaLoader). Together with the
	// vendored, pre-registered sub-schemas this guarantees validation never silently
	// depends on cyclonedx.org being reachable; a missing sub-schema fails compilation
	// here rather than being downloaded at startup.
	compiler.RegisterLoader("http", noNetworkSchemaLoader)
	compiler.RegisterLoader("https", noNetworkSchemaLoader)
	for _, sub := range subSchemaFiles {
		sb, err := schemas.ReadFile(sub)
		if err != nil {
			return Service{}, fmt.Errorf("failed to read embedded sub-schema %s: %w", sub, err)
		}
		if _, err := compiler.Compile(sb); err != nil {
			return Service{}, fmt.Errorf("failed to compile embedded sub-schema %s: %w", sub, err)
		}
	}

	jsonSchemas := make(map[string]*jss.Schema)
	for version, filename := range versionToEmbeddedFileMapping {
		b, err := schemas.ReadFile(filename)
		if err != nil {
			return Service{}, fmt.Errorf("failed to read embedded file %s: %w", filename, err)
		}

		schema, err := compiler.Compile(b)
		if err != nil {
			return Service{}, fmt.Errorf("failed to compile schema %s (version %s): %w", filename, version, err)
		}

		// Fail closed: any still-unresolved external reference would make
		// validation silently fail-open (accept BOMs that violate the
		// referenced sub-schema). Refuse to start rather than validate
		// against an incomplete schema — this typically means a referenced
		// sub-schema is missing from subSchemaFiles.
		if unresolved := schema.UnresolvedReferenceURIs(); len(unresolved) > 0 {
			return Service{}, fmt.Errorf("schema %s (version %s) has unresolved references (missing vendored sub-schema?): %v", filename, version, unresolved)
		}

		jsonSchemas[version] = schema
	}

	return Service{
		jsonSchemas: jsonSchemas,
		store:       store,
		config:      config,
	}, nil
}

func (s Service) SupportedVersion() []string {
	return slices.Sorted(maps.Keys(s.jsonSchemas))
}

func (s Service) VersionSupported(version string) bool {
	if _, ok := s.jsonSchemas[version]; ok {
		return true
	}
	return false
}

type SearchRes struct {
	SerialNumber string      `json:"serialNumber"`
	Version      string      `json:"version"`
	Timestamp    string      `json:"created_at"`
	CryptoStats  CryptoStats `json:"cryptoStats"`
}

// Search retrieves all BOMs with a last modified timestamp greater than the specified value.
// The function queries the underlying store for matching BOMs and enriches each result with
// cryptographic asset statistics extracted from object metadata.
//
// Parameters:
//   - ctx: Context for cancellation, deadlines, and additional slog fields.
//   - ts: Unix timestamp (seconds since epoch); only BOMs modified after this time are returned
//
// Returns:
//   - []SearchRes: Slice of search results containing serial number, version, timestamp, and crypto statistics
//   - error: Non-nil if the store query fails, key format is invalid, or JSON unmarshaling fails
func (s Service) Search(ctx context.Context, ts int64) ([]SearchRes, error) {
	res := []SearchRes{}

	ctx = log.ContextAttrs(ctx, slog.Int64("timestamp", ts))
	slog.DebugContext(ctx, "Calling `store.Search()`.")

	r, err := s.store.Search(ctx, ts)
	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, "`store.Search()` finished.",
		slog.Int("count", len(r)),
		slog.String("value", strings.Join(r, ",")),
	)

	for _, cpy := range r {
		idx := strings.LastIndex(cpy, "-")
		if idx == -1 {
			slog.ErrorContext(ctx, "Key does NOT adhere to the naming invariant.",
				slog.String("key", cpy), slog.String("expected-format", "urn:uuid:<uuid>-<version>"))
			return nil, errors.New("unexpected key returned from store")
		}

		head, err := s.store.GetHeadObject(ctx, cpy)
		switch {
		case errors.Is(err, store.ErrNotFound):
			slog.WarnContext(ctx, fmt.Sprintf("Fetching HeadObject for key %q failed although version was previously returned by `store.Search()`. Skipping from result set.", cpy))
			continue

		case err != nil:
			return nil, err
		}

		cryptoStatsValue, ok := head.Metadata[store.MetaCryptoStatsKey]
		if !ok {
			slog.WarnContext(ctx,
				fmt.Sprintf("There is no key %q in object metadata. Skipping from result set.", store.MetaCryptoStatsKey),
				slog.String("object-key", cpy))
			continue
		}

		var cryptoStats CryptoStats
		if err := json.Unmarshal([]byte(cryptoStatsValue), &cryptoStats); err != nil {
			slog.ErrorContext(ctx,
				fmt.Sprintf("Unmarshaling metadata key %q value failed.", store.MetaCryptoStatsKey),
				slog.String("error", err.Error()), slog.String("object-key", cpy))
			return res, errors.New("unmarshaling json failed")
		}

		res = append(res, SearchRes{
			SerialNumber: cpy[:idx],
			Version:      cpy[idx+1:],
			Timestamp:    head.LastModified.Format(time.RFC3339),
			CryptoStats:  cryptoStats,
		})
	}
	return res, nil
}

// GetBOMByUrn retrieves a BOM document by its URN and version.
//
// The function returns the BOM as a byte slice to preserve the original JSON structure
// and allow flexible handling of different CycloneDX schema versions as well as allowing
// callers to decide, through service configuration, whether check the contents received
// from backend storage or not.
//
// Version Selection:
//   - If version is specified: Retrieves that specific version
//   - If version is empty: Automatically selects and retrieves the latest version
//
// Parameters:
//   - ctx: Context for cancellation, deadlines, and additional slog fields
//   - urn: The URN identifier of the BOM (format: urn:uuid:<uuid>)
//   - version: The specific version to retrieve, or empty string for latest version
//
// Returns:
//   - []byte: The BOM document as a byte slice
//   - error: Returns ErrNotFound if the URN or version doesn't exist,
//     or other errors from the store or JSON unmarshaling
func (s Service) GetBOMByUrn(ctx context.Context, urn, version string) ([]byte, error) {
	ctx = log.ContextAttrs(ctx,
		slog.String("urn", urn),
		slog.String("version", version),
	)

	if strings.TrimSpace(version) == "" {
		slog.DebugContext(ctx, "Version is empty, calling `store.GetObjectVersions()` to obtain the latest BOM version stored.")
		versions, hasOriginal, err := s.store.GetObjectVersions(ctx, urn)
		switch {
		case errors.Is(err, store.ErrNotFound):
			return nil, ErrNotFound

		case err != nil:
			return nil, err
		}

		version = fmt.Sprintf("%d", versions[len(versions)-1])
		slog.DebugContext(ctx, "Latest version selected.", slog.Group("getObjectVersionsResult",
			slog.Any("all-versions", versions),
			slog.String("selected-version", version),
			slog.Bool("has-original", hasOriginal),
		),
		)
		ctx = log.ContextAttrs(ctx, slog.String("selected-version", version))
	}

	slog.DebugContext(ctx, "Calling `store.GetObject()`.")
	b, err := s.store.GetObject(ctx, fmt.Sprintf("%s-%s", urn, version))
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil, ErrNotFound

	case err != nil:
		return nil, err
	}
	slog.DebugContext(ctx, "`store.GetObject()` finished.", slog.Int64("size", int64(len(b))))

	if s.config.CheckOnFetch {
		var bomMap map[string]interface{}
		if err := json.Unmarshal(b, &bomMap); err != nil {
			slog.ErrorContext(
				ctx,
				"`json.Unmarshal()` failed while checking the contents returned form the backend storage.", slog.String("error", err.Error()))
			return nil, errors.New("BOM fetched from backend storage is malformed")
		}
	}

	return b, nil
}

type VersionRes struct {
	Version     string      `json:"version"`
	Timestamp   string      `json:"created_at"`
	CryptoStats CryptoStats `json:"cryptoStats"`
}

// UrnVersions retrieves all available versions of a BOM identified by its URN.
// The function returns some metadata for each version including the version
// identifier, last modified timestamp, and cryptographic asset statistics.
//
// The returned slice includes all numbered versions (e.g., "1", "2", "3") and
// may also include an "original" version if one exists in the store. Versions
// that exist in the store but are missing required metadata (such as crypto
// statistics) are logged as warnings and excluded from the results.
//
// Parameters:
//   - ctx: Context for cancellation, deadlines, and additional slog fields
//   - urn: The URN identifier of the BOM (format: urn:uuid:<uuid>)
//
// Returns:
//   - []VersionRes: Slice of versions, some metadata and crypto statistics
//   - error: Returns ErrNotFound if the URN doesn't exist, or other errors
//     from the store or JSON unmarshaling
func (s Service) UrnVersions(ctx context.Context, urn string) ([]VersionRes, error) {
	ctx = log.ContextAttrs(ctx,
		slog.String("urn", urn),
	)

	versions, hasOriginal, err := s.store.GetObjectVersions(ctx, urn)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil, ErrNotFound

	case err != nil:
		return nil, err
	}

	var toProcess []string
	for _, cpy := range versions {
		toProcess = append(toProcess, strconv.Itoa(cpy))
	}
	if hasOriginal {
		toProcess = append(toProcess, "original")
	}

	res := []VersionRes{}
	for _, cpy := range toProcess {
		key := fmt.Sprintf("%s-%s", urn, cpy)
		head, err := s.store.GetHeadObject(ctx, key)
		switch {
		case errors.Is(err, store.ErrNotFound):
			slog.WarnContext(ctx, "Fetching HeadObject for key failed although it was previously returned by `store.GetObjectVersions()`. Skipping from result set.",
				slog.String("key", key))
			continue

		case err != nil:
			return nil, err
		}

		cryptoStats, ok := head.Metadata[store.MetaCryptoStatsKey]
		if !ok {
			slog.WarnContext(ctx, fmt.Sprintf("There is no key %q in object metadata. Skipping from result set.", store.MetaCryptoStatsKey),
				slog.String("object-key", key))
			continue
		}

		item := VersionRes{
			Version:   cpy,
			Timestamp: head.LastModified.Format(time.RFC3339),
		}
		if err := json.Unmarshal([]byte(cryptoStats), &item.CryptoStats); err != nil {
			slog.ErrorContext(ctx, fmt.Sprintf("Unmarshaling value of metadata key %q failed.", store.MetaCryptoStatsKey),
				slog.String("error", err.Error()), slog.String("object-key", key))
			return res, errors.New("unmarshaling json failed")
		}
		res = append(res, item)
	}
	return res, nil
}

// URNValid returns true if `urn` is a valid URN conforming to RFC-4122.
// URN format is defined as `urn:<NID>:<NSS>`
// where:
//   - <NID> means Namespace Identifier. For RFC-4122 this means exactly "uuid" string.
//   - <NSS> means Namespace Specific String. For RFC-4122 this means a valid UUID.
func URNValid(urn string) bool {
	subs := strings.Split(urn, ":")
	if len(subs) != 3 {
		return false
	}
	if subs[0] != "urn" || subs[1] != "uuid" {
		return false
	}
	if _, err := uuid.Parse(subs[2]); err != nil {
		return false
	}
	return true
}
