package http

import (
	"fmt"

	"github.com/CZERTAINLY/CBOM-Repository/internal/service"
)

// ValidateDefaultBOMVersion returns an error if the configured default upload
// version is not supported by the service. Callers should abort startup on error.
func ValidateDefaultBOMVersion(cfg Config, svc service.Service) error {
	if !svc.VersionSupported(cfg.DefaultBOMVersion) {
		return fmt.Errorf("configured APP_DEFAULT_BOM_VERSION %q is not supported (supported: %v)",
			cfg.DefaultBOMVersion, svc.SupportedVersion())
	}
	return nil
}
