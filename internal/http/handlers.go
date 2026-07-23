package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/OmniTrustILM/cbom-repository/internal/service"

	"github.com/gorilla/mux"
)

func (h Server) Upload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Assert content type and optional version
	ok, version := CheckContentType(r.Header.Get(HeaderContentType), h.cfg.DefaultBOMVersion)
	if !ok {
		unsupportedMediaType(w,
			fmt.Sprintf("Content type value '%s' not allowed for path '%s' and method '%s'. Supported content types: %s",
				r.Header.Get(HeaderContentType), r.URL.Path, r.Method, []string{"application/vnd.cyclonedx+json"}))
		return
	}

	if !h.service.VersionSupported(version) {
		badrequest(w, fmt.Sprintf("Version '%s' not supported, supported versions: %s", version, h.service.SupportedVersion()))
		return
	}

	slog.InfoContext(ctx, "Start.")

	var maxErr *http.MaxBytesError
	resp, err := h.service.UploadBOM(ctx, r.Body, version)
	switch {
	case errors.As(err, &maxErr):
		requestTooLarge(w, "HTTP request body exceeded the maximum allowed size.")
		return

	case errors.Is(err, service.ErrAlreadyExists):
		conflict(w, fmt.Sprintf(
			"Conflict with existing BOM, serial number '%s', version '%d'.",
			resp.SerialNumber, resp.Version))
		return

	case errors.Is(err, service.ErrValidation):
		badrequest(w, fmt.Sprintf("Validating BOM failed: %s", err))
		return

	case err != nil:
		internal(w, fmt.Sprintf("Uploading BOM failed: %s", err))
		return
	}

	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err = json.NewEncoder(w).Encode(resp); err != nil {
		slog.ErrorContext(ctx, "`json.NewEncoder()` failed", slog.String("error", err.Error()))
		return
	}
	slog.InfoContext(ctx, "Finished.", slog.Group(
		"response",
		slog.String("serialNumber", resp.SerialNumber),
		slog.Int("version", resp.Version),
	))
}

func (s Server) GetByURN(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	urn := vars["urn"]

	if !validateURNPathVariable(w, urn) {
		return
	}

	version := r.URL.Query().Get("version")

	slog.InfoContext(ctx, "Start.", slog.String("urn", urn), slog.String("version", version))

	resp, err := s.service.GetBOMByUrn(ctx, urn, version)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrNotFound):
			notfound(w, "Requested BOM not found.")
			return
		}

		internal(w, fmt.Sprintf("Failed to get the requested BOM: %s.", err))
		return
	}

	w.Header().Set("Content-Type", "application/vnd.cyclonedx+json")
	if _, err := w.Write(resp); err != nil {
		slog.ErrorContext(ctx, "Writing to http.ResponseWriter failed.", slog.String("error", err.Error()))
		return
	}
	slog.InfoContext(ctx, "Finished.")
}

func validateURNPathVariable(w http.ResponseWriter, urn string) bool {
	if !service.URNValid(urn) {
		badrequest(w, fmt.Sprintf("Path variable `{urn}` has invalid value: %q. Valid value MUST have the following structure: 'urn:uuid:<uuid>'.", urn))
		return false
	}
	return true
}

func (s Server) URNVersions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	urn := vars["urn"]

	if !validateURNPathVariable(w, urn) {
		return
	}

	slog.InfoContext(ctx, "Start.", slog.String("urn", urn))

	resp, err := s.service.UrnVersions(ctx, urn)
	switch {
	case errors.Is(err, service.ErrNotFound):
		notfound(w, "No versions found for requested serial number.")
		return

	case err != nil:
		internal(w, fmt.Sprintf("Failed to get versions for requested serial number: %s", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err = json.NewEncoder(w).Encode(resp); err != nil {
		slog.ErrorContext(ctx, "`json.NewEncoder()` failed", slog.String("error", err.Error()))
		return
	}
	slog.InfoContext(ctx, "Finished.")
}

func (h Server) Search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	after := r.URL.Query().Get("after")

	if strings.TrimSpace(after) == "" {
		badrequest(w, "Request validation failed, query parameter 'after' must not be empty.")
		return
	}

	i, err := strconv.ParseInt(after, 10, 64)
	if err != nil || i < 0 {
		badrequest(w, "Request validation failed, query parameter 'after' must be a positive integer (unixtime).")
		return
	}

	slog.InfoContext(ctx, "Start.", slog.String("after", after))

	resp, err := h.service.Search(ctx, i)
	if err != nil {
		internal(w, fmt.Sprintf("Failed to get the requested BOM: %s.", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err = json.NewEncoder(w).Encode(resp); err != nil {
		slog.ErrorContext(ctx, "`json.NewEncoder()` failed", slog.String("error", err.Error()))
		return
	}
	slog.InfoContext(ctx, "Finished.", slog.Int("response-count", len(resp)))
}
