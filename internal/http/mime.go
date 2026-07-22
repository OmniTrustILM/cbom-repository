package http

import (
	"mime"
	"strings"
)

// HeaderContentType is the canonical key used when reading the request header for content type.
const HeaderContentType = "content-type"

// CheckContentType validates the media type and returns the requested CycloneDX
// version. When the `version` media-type parameter is absent, defaultVersion is used.
func CheckContentType(contentType, defaultVersion string) (bool, string) {
	if strings.TrimSpace(contentType) == "" {
		return false, ""
	}

	t, p, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false, ""
	}
	if t != "application/vnd.cyclonedx+json" {
		return false, ""
	}
	version, ok := p["version"]
	if !ok {
		version = defaultVersion
	}

	return true, version
}
