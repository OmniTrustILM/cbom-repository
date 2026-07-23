package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/OmniTrustILM/cbom-repository/internal/health"
	"github.com/OmniTrustILM/cbom-repository/internal/log"
	"github.com/OmniTrustILM/cbom-repository/internal/service"

	"github.com/gorilla/mux"
)

const (
	V1Prefix         = "/v1"
	RouteBOM         = V1Prefix + "/bom"
	RouteBOMByURN    = RouteBOM + "/{urn}"
	RouteBOMVersions = RouteBOMByURN + "/versions"
	RouteHealth      = V1Prefix + "/health"
	RouteHealthLive  = RouteHealth + "/liveness"
	RouteHealthReady = RouteHealth + "/readiness"
)

type Config struct {
	Port   int    `envconfig:"APP_HTTP_PORT" default:"8080"`
	Prefix string `envconfig:"APP_HTTP_PREFIX" default:"/api"`
	// default HTTP request body size is 20 MiB
	MaxBodySize int64 `envconfig:"APP_HTTP_MAX_BODY_SIZE" default:"20971520"`
}

type Server struct {
	cfg           Config
	service       service.Service
	healthService health.Service
}

func New(cfg Config, svc service.Service, healthSvc health.Service) Server {
	cfg.Prefix = strings.TrimSuffix(cfg.Prefix, "/")
	if len(cfg.Prefix) != 0 && cfg.Prefix[0] != '/' {
		cfg.Prefix = fmt.Sprintf("/%s", cfg.Prefix)
	}

	return Server{
		cfg:           cfg,
		service:       svc,
		healthService: healthSvc,
	}
}

func (s *Server) Handler() *mux.Router {
	r := mux.NewRouter()

	r.Use(maxBodySizeMiddleware(s.cfg.MaxBodySize))
	r.Use(httpInfoContext)

	r.HandleFunc(fmt.Sprintf("%s%s", s.cfg.Prefix, RouteBOM), s.Upload).Methods(http.MethodPost)
	r.HandleFunc(fmt.Sprintf("%s%s", s.cfg.Prefix, RouteBOM), s.Search).Methods(http.MethodGet)
	r.HandleFunc(fmt.Sprintf("%s%s", s.cfg.Prefix, RouteBOMByURN), s.GetByURN).Methods(http.MethodGet)
	r.HandleFunc(fmt.Sprintf("%s%s", s.cfg.Prefix, RouteBOMVersions), s.URNVersions).Methods(http.MethodGet)
	r.HandleFunc(fmt.Sprintf("%s%s", s.cfg.Prefix, RouteHealth), s.HealthHandler).Methods(http.MethodGet)
	r.HandleFunc(fmt.Sprintf("%s%s", s.cfg.Prefix, RouteHealthLive), s.LivenessHandler).Methods(http.MethodGet)
	r.HandleFunc(fmt.Sprintf("%s%s", s.cfg.Prefix, RouteHealthReady), s.ReadinessHandler).Methods(http.MethodGet)

	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("Received an HTTP request for an unmapped path and method.",
			slog.String("path", r.URL.Path), slog.String("method", r.Method))
		notfound(w, fmt.Sprintf("There is no handler registered for path: %s, method: %s", r.URL.Path, r.Method))
	})

	return r
}

// HealthHandler handles requests to the /api/v1/health endpoint.
// It returns the overall health status of the service and its components.
// Returns 200 OK if status is UP or DEGRADED, 503 Service Unavailable otherwise.
func (h Server) HealthHandler(w http.ResponseWriter, r *http.Request) {
	healthStatus := h.healthService.CheckHealth(r.Context())

	statusCode := http.StatusOK
	if healthStatus.Status == health.StatusDown || healthStatus.Status == health.StatusOutOfService {
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(healthStatus); err != nil {
		slog.ErrorContext(r.Context(), "`json.NewEncoder()` failed", slog.String("error", err.Error()))
		return
	}
}

// LivenessHandler handles requests to the /api/v1/health/liveness endpoint.
// It returns the liveness status used by Kubernetes to determine if the pod should be restarted.
// Always returns 200 OK with status UP unless the application process is in a failed state.
func (h Server) LivenessHandler(w http.ResponseWriter, r *http.Request) {
	healthStatus := h.healthService.CheckLiveness(r.Context())

	statusCode := http.StatusOK
	if healthStatus.Status != health.StatusUp {
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(healthStatus); err != nil {
		slog.ErrorContext(r.Context(), "`json.NewEncoder()` failed", slog.String("error", err.Error()))
		return
	}
}

// ReadinessHandler handles requests to the /api/v1/health/readiness endpoint.
// It returns the readiness status used by Kubernetes to determine if the pod can accept traffic.
// Returns 200 OK if all critical components are available, 503 Service Unavailable otherwise.
func (h Server) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	healthStatus := h.healthService.CheckReadiness(r.Context())

	statusCode := http.StatusOK
	if healthStatus.Status != health.StatusUp {
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(healthStatus); err != nil {
		slog.ErrorContext(r.Context(), "`json.NewEncoder()` failed", slog.String("error", err.Error()))
		return
	}
}

func httpInfoContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		// Add structured HTTP attributes to context
		ctx := log.ContextAttrs(r.Context(), slog.Group("http-info",
			slog.String("method", r.Method),
			slog.String("url-path", r.URL.Path),
		))

		// Pass updated request into chain
		r = r.WithContext(ctx)
		next.ServeHTTP(w, r)
	})
}

// maxBodySizeMiddleware limits the size of the request body to maxBytes.
func maxBodySizeMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Wrap the request body with a MaxBytesReader
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

			// Continue to the next handler
			next.ServeHTTP(w, r)
		})
	}
}
