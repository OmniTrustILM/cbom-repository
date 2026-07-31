package env

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/OmniTrustILM/cbom-repository/internal/http"
	"github.com/OmniTrustILM/cbom-repository/internal/service"
	"github.com/OmniTrustILM/cbom-repository/internal/store"

	"github.com/kelseyhightower/envconfig"
)

const defaultPrefix = "APP"

type Config struct {
	Store    store.Config
	Http     http.Config
	LogLevel slog.Level `envconfig:"APP_LOG_LEVEL" default:"INFO"`
	Service  service.Config
}

func New() (Config, error) {
	var config Config
	err := envconfig.Process(defaultPrefix, &config)
	if err != nil {
		return Config{}, err
	}

	if strings.TrimSpace(config.Store.Region) == "" {
		return Config{}, errors.New("environment variable `APP_S3_REGION` must not contain whitespace characters only")
	}

	if strings.TrimSpace(config.Store.Bucket) == "" {
		return Config{}, errors.New("environment variable `APP_S3_BUCKET` must not contain whitespace characters only")
	}

	if strings.TrimSpace(config.Store.AccessKey) == "" {
		return Config{}, errors.New("environment variable `APP_S3_ACCESS_KEY` must not contain whitespace characters only")
	}

	if strings.TrimSpace(config.Store.SecretKey) == "" {
		return Config{}, errors.New("environment variable `APP_S3_SECRET_KEY` must not contain whitespace characters only")
	}

	if config.Http.MaxBodySize <= 0 {
		return Config{}, errors.New("environment variable `APP_HTTP_MAX_BODY_SIZE` must be an integer greater than zero")
	}

	for _, origin := range config.Http.CORSAllowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" {
			continue
		}

		// A browser serializes Origin as exactly scheme://host[:port], so an
		// entry is usable only if it already is that and nothing more. Compare
		// against the canonical form rather than field by field: it also
		// catches what individual fields leave invisible, such as the bare
		// delimiters in `https://ilm.example.net?` and `https://ilm.example.net#`.
		// The comparison ignores case, since matching does too.
		u, err := url.Parse(origin)
		if err != nil || u.Scheme == "" || u.Host == "" ||
			!strings.EqualFold(origin, u.Scheme+"://"+u.Host) {
			return Config{}, fmt.Errorf(
				"environment variable `APP_HTTP_CORS_ALLOWED_ORIGINS` must list scheme://host[:port] entries or `*`, got %q", origin)
		}
	}

	return config, nil
}
