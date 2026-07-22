package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/CZERTAINLY/CBOM-Repository/internal/log"
	"github.com/CZERTAINLY/CBOM-Repository/internal/store"

	cdx "github.com/CycloneDX/cyclonedx-go"
	"github.com/google/uuid"
)

type BOMCreated struct {
	SerialNumber string      `json:"serialNumber"`
	Version      int         `json:"version"`
	CryptoStats  CryptoStats `json:"cryptoStats"`
}

// UploadBOM processes and stores a CycloneDX BOM (Bill of Materials) document.
// The function handles three distinct scenarios based on the BOM's serial number and version fields:
//
//  1. Missing serial number: Generates a new RFC-4122 compliant URN, sets version to 1,
//     and stores both the original BOM and a modified version with the generated fields.
//
//  2. Valid serial number with invalid version (< 1): If a BOM with this serial number already
//     exists, fetches existing versions for the serial number and assigns the next sequential
//     version number, otherwise assigns version 1. Stores the modified BOM with the updated
//     version field.
//
//  3. Valid serial number and version: Verifies the BOM doesn't already exist and stores
//     it as-is. Returns ErrAlreadyExists if a BOM with the same serial number and version
//     already exists.
//
// Cryptographic asset statistics are calculated for all uploaded BOMs and stored
// as metadata alongside the BOM document.
//
// Parameters:
//   - ctx: Context for cancellation, deadlines and additional slog fields.
//   - rc: Reader containing the BOM document (will be closed by this function)
//   - schemaVersion: Expected CycloneDX schema version (e.g., "1.6" or "1.7")
//
// Returns:
//   - BOMCreated: Contains the serial number, version, and crypto statistics of the stored BOM
//   - error: ErrValidation if validation fails, ErrAlreadyExists if the BOM already exists,
//     or other errors from decoding, encoding, or storage operations
func (s Service) UploadBOM(ctx context.Context, rc io.ReadCloser, schemaVersion string) (BOMCreated, error) {

	var buf bytes.Buffer
	tee := io.TeeReader(rc, &buf)
	defer func() {
		_ = rc.Close()
	}()

	ctx = log.ContextAttrs(ctx, slog.String("declared-bom-schema-version", schemaVersion))

	var bom cdx.BOM
	decoder := cdx.NewBOMDecoder(tee, cdx.BOMFileFormatJSON)
	if err := decoder.Decode(&bom); err != nil {
		slog.ErrorContext(ctx, "`cdx.Decode()` failed.", slog.String("error", err.Error()))
		// Wrap as ErrValidation (-> 400) while keeping any *http.MaxBytesError reachable
		// via errors.As, so an oversized body is still classified 413 by the handler.
		return BOMCreated{}, fmt.Errorf("%w: %w", ErrValidation, err)
	}

	if err := uploadInputChecks(bom, schemaVersion); err != nil {
		return BOMCreated{}, fmt.Errorf("%w: %s", ErrValidation, err)
	}

	jsonSchema, ok := s.jsonSchemas[schemaVersion]
	if !ok {
		// Reachable only via a direct service call (the HTTP layer gates on VersionSupported).
		// Treat as a validation error so it maps to 400, not 500.
		slog.ErrorContext(ctx, "Missing schema validator!!!", slog.String("version", schemaVersion))
		return BOMCreated{}, fmt.Errorf("%w: no schema validator for version %s", ErrValidation, schemaVersion)
	}

	res := jsonSchema.Validate(buf.Bytes())
	if !res.IsValid() {
		return BOMCreated{}, fmt.Errorf("%w: does not conform to the declared schema", ErrValidation)
	}

	cryptoStats := CalculateCryptoStats(ctx, &bom)
	b, err := json.Marshal(cryptoStats)
	if err != nil {
		return BOMCreated{}, fmt.Errorf("`json.Marshal()` failed: %w", err)
	}

	var retVal BOMCreated
	var retErr error
	switch {
	case bom.SerialNumber == "":
		retVal, retErr = s.uploadCaseSNInvalid(ctx, bom, buf, string(b))

	case bom.Version < 1:
		retVal, retErr = s.uploadCaseSNValidVersionInvalid(ctx, bom, string(b))

	default:
		// serial number of the BOM is valid, version is set
		retVal, retErr = s.uploadCaseSNValidVersionValid(ctx, bom, buf, string(b))
	}
	if retErr == nil {
		retVal.CryptoStats = cryptoStats
	}
	return retVal, retErr
}

func (s Service) uploadCaseSNInvalid(ctx context.Context, bom cdx.BOM, orig bytes.Buffer, cryptoStats string) (BOMCreated, error) {
	slog.DebugContext(ctx, "BOM does not have serial number specified - generating a new one.")
	// serial number is missing, so we're going to generate a unique new one,
	// that means this will be version 1, even if something else was set
	bom.Version = 1

	for {
		// generate a new urn and make sure we don't conflict with an existing one
		bom.SerialNumber = fmt.Sprintf("urn:uuid:%s", uuid.NewString())
		exists, err := s.store.KeyExists(ctx, uploadKey(bom.SerialNumber, bom.Version))
		if err != nil {
			return BOMCreated{}, err
		}
		if !exists {
			break
		}
	}
	ctx = log.ContextAttrs(ctx, slog.String("new-serial-number", bom.SerialNumber))
	slog.DebugContext(ctx, "New serial number generated.")

	// store the original unchanged BOM
	metaOriginal := store.Metadata{
		Version:     "original",
		CryptoStats: cryptoStats,
	}
	if err := s.store.Upload(ctx, uploadKeyOriginal(bom.SerialNumber), metaOriginal, orig.Bytes()); err != nil {
		return BOMCreated{}, err
	}
	slog.DebugContext(ctx, "Stored original BOM.")

	// store the modified BOM with serialNumber and version set
	meta := store.Metadata{
		Version:     fmt.Sprintf("%d", bom.Version),
		CryptoStats: cryptoStats,
	}

	var modifiedBuf bytes.Buffer
	encoder := cdx.NewBOMEncoder(&modifiedBuf, cdx.BOMFileFormatJSON)
	if err := encoder.Encode(&bom); err != nil {
		slog.ErrorContext(ctx, "`cdx.Encode()` failed.", slog.String("error", err.Error()))
		return BOMCreated{}, err
	}

	if err := s.store.Upload(ctx, uploadKey(bom.SerialNumber, bom.Version), meta, modifiedBuf.Bytes()); err != nil {
		return BOMCreated{}, err
	}
	slog.DebugContext(ctx, "Stored modified version.")

	return BOMCreated{
		SerialNumber: bom.SerialNumber,
		Version:      bom.Version,
	}, nil
}

func (s Service) uploadCaseSNValidVersionInvalid(ctx context.Context, bom cdx.BOM, cryptoStats string) (BOMCreated, error) {
	slog.DebugContext(ctx, "BOM has only serial number specified - fetching the latest version")
	versions, hasOriginal, err := s.store.GetObjectVersions(ctx, bom.SerialNumber)
	switch {
	case errors.Is(err, store.ErrNotFound):
		bom.Version = 1
		slog.DebugContext(ctx, "First BOM with this SN, assigning Version '1'.")
	case err != nil:
		return BOMCreated{}, err
	default:
		bom.Version = versions[len(versions)-1] + 1
		slog.DebugContext(ctx, "New version assigned to BOM.",
			slog.Int("new-version", bom.Version),
			slog.Any("all-versions", versions),
			slog.Bool("has-original", hasOriginal),
		)
	}

	meta := store.Metadata{
		Version:     fmt.Sprintf("%d", bom.Version),
		CryptoStats: cryptoStats,
	}

	var modifiedBuf bytes.Buffer
	encoder := cdx.NewBOMEncoder(&modifiedBuf, cdx.BOMFileFormatJSON)
	if err = encoder.Encode(&bom); err != nil {
		return BOMCreated{}, err
	}

	if err := s.store.Upload(ctx, uploadKey(bom.SerialNumber, bom.Version), meta, modifiedBuf.Bytes()); err != nil {
		return BOMCreated{}, err
	}
	slog.DebugContext(ctx, "Stored modified BOM.")
	return BOMCreated{
		SerialNumber: bom.SerialNumber,
		Version:      bom.Version,
	}, nil
}

func (s Service) uploadCaseSNValidVersionValid(ctx context.Context, bom cdx.BOM, orig bytes.Buffer, cryptoStats string) (BOMCreated, error) {
	slog.DebugContext(ctx, "BOM has serial number and version specified.")
	// let's make sure it doesn't exist already
	exists, err := s.store.KeyExists(ctx, uploadKey(bom.SerialNumber, bom.Version))
	if err != nil {
		return BOMCreated{}, err
	}
	if exists {
		return BOMCreated{
			SerialNumber: bom.SerialNumber,
			Version:      bom.Version,
		}, ErrAlreadyExists
	}

	meta := store.Metadata{
		Version:     fmt.Sprintf("%d", bom.Version),
		CryptoStats: cryptoStats,
	}

	if err := s.store.Upload(ctx, uploadKey(bom.SerialNumber, bom.Version), meta, orig.Bytes()); err != nil {
		return BOMCreated{}, err
	}
	slog.DebugContext(ctx, "Stored original BOM")

	return BOMCreated{
		SerialNumber: bom.SerialNumber,
		Version:      bom.Version,
	}, nil
}

// uploadInputChecks returns error in case BOM fails any of the input checks,
// nil otherwise.
func uploadInputChecks(bom cdx.BOM, expectedVersion string) error {
	if bom.BOMFormat != cdx.BOMFormat {
		return fmt.Errorf("required format %s", cdx.BOMFormat)
	}
	// if the serial number is set, it must be a valid URN conforming to RFC 4122
	if bom.SerialNumber != "" && !URNValid(bom.SerialNumber) {
		return fmt.Errorf("serial number not valid")
	}

	cdxVersion, err := knownCdxVersion(expectedVersion)
	if err != nil {
		return err
	}
	if bom.SpecVersion != cdxVersion {
		return fmt.Errorf("required version %s", expectedVersion)
	}

	return nil
}

func uploadKey(urn string, version int) string {
	return fmt.Sprintf("%s-%d", urn, version)
}

func uploadKeyOriginal(urn string) string {
	return fmt.Sprintf("%s-original", urn)
}

func knownCdxVersion(v string) (cdx.SpecVersion, error) {
	switch v {
	case "1.0":
		return cdx.SpecVersion1_0, nil
	case "1.1":
		return cdx.SpecVersion1_1, nil
	case "1.2":
		return cdx.SpecVersion1_2, nil
	case "1.3":
		return cdx.SpecVersion1_3, nil
	case "1.4":
		return cdx.SpecVersion1_4, nil
	case "1.5":
		return cdx.SpecVersion1_5, nil
	case "1.6":
		return cdx.SpecVersion1_6, nil
	case "1.7":
		return cdx.SpecVersion1_7, nil
	default:
		return -1, fmt.Errorf("unknown cyclonedx bom version %s", v)
	}
}
