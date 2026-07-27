package http

import (
	"mime"
	"strings"
)

// HeaderContentType is the canonical key used when reading the request header for content type.
const HeaderContentType = "content-type"

// CheckContentType validates the media type and returns the CycloneDX version
// requested via the optional `version` media-type parameter. The returned version
// is empty when the parameter is absent, in which case the upload path auto-detects
// the version from the document's own specVersion.
func CheckContentType(contentType string) (bool, string) {
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

	return true, p["version"]
}
