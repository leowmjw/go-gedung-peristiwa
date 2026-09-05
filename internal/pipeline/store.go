package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Backend string

const (
	BackendMemory Backend = "memory"
	BackendMinIO  Backend = "minio"
	BackendTigris Backend = "tigris"
)

// Change-feed history retention bounds. ChangeFeedRetainFor below
// DefaultChangeFeedRetainFor (e.g. zero/unset) defaults to 30 days; anything
// above MaxChangeFeedRetainFor is clamped to one year. This bounds how far
// back historical replay (internal/demo/replay.go) can walk the change feed
// before maintenance GC has reclaimed it — see IsleDB Learnings in AGENTS.md.
const (
	DefaultChangeFeedRetainFor = 30 * 24 * time.Hour
	MaxChangeFeedRetainFor     = 365 * 24 * time.Hour
)

// StoreConfig configures object storage backends.
type StoreConfig struct {
	Backend      Backend
	PrefixRoot   string // optional suffix appended to tenant prefix (e.g. run id)
	CacheRoot    string
	MinEndpoint  string
	MinBucket    string
	TigrisBucket string

	// ChangeFeedRetainFor overrides how long change-feed history is kept
	// before maintenance GC reclaims it. Zero selects DefaultChangeFeedRetainFor;
	// values above MaxChangeFeedRetainFor are clamped. Use NormalizeChangeFeedRetainFor.
	ChangeFeedRetainFor time.Duration
}

func StoreConfigFromEnv(backend Backend, prefixSuffix string) StoreConfig {
	cfg := StoreConfig{
		Backend:             backend,
		PrefixRoot:          prefixSuffix,
		CacheRoot:           DefaultCacheRoot,
		MinEndpoint:         envOr("MINIO_ENDPOINT", "localhost:9000"),
		MinBucket:           envOr("MINIO_BUCKET", "gedung-peristiwa"),
		TigrisBucket:        envOr("TIGRIS_BUCKET", envOr("MINIO_BUCKET", "gedung-peristiwa")),
		ChangeFeedRetainFor: changeFeedRetainForFromEnv("CHANGEFEED_RETAIN_FOR"),
	}
	return cfg
}

// NormalizeChangeFeedRetainFor applies the default/max bounds documented on
// DefaultChangeFeedRetainFor / MaxChangeFeedRetainFor. Safe to call on a zero
// value (StoreConfig built without StoreConfigFromEnv, e.g. in tests).
func NormalizeChangeFeedRetainFor(d time.Duration) time.Duration {
	if d <= 0 {
		return DefaultChangeFeedRetainFor
	}
	if d > MaxChangeFeedRetainFor {
		return MaxChangeFeedRetainFor
	}
	return d
}

func changeFeedRetainForFromEnv(key string) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return DefaultChangeFeedRetainFor
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return DefaultChangeFeedRetainFor
	}
	return NormalizeChangeFeedRetainFor(d)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func tenantPrefix(tenantID, suffix string) string {
	base := tenantID
	if suffix != "" {
		base = tenantID + "-" + suffix
	}
	return base
}

// ensureS3Env maps MinIO credentials to AWS env vars for gocloud s3blob.
func ensureS3Env(cfg StoreConfig) {
	if os.Getenv("AWS_ACCESS_KEY_ID") == "" {
		if v := os.Getenv("MINIO_ACCESS_KEY"); v != "" {
			os.Setenv("AWS_ACCESS_KEY_ID", v)
		}
	}
	if os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {
		if v := os.Getenv("MINIO_SECRET_KEY"); v != "" {
			os.Setenv("AWS_SECRET_ACCESS_KEY", v)
		}
	}
	if cfg.Backend == BackendMinIO {
		os.Setenv("AWS_S3_USE_PATH_STYLE", "true")
		if os.Getenv("AWS_REGION") == "" {
			os.Setenv("AWS_REGION", "us-east-1")
		}
	}
	if os.Getenv("AWS_REGION") == "" && cfg.Backend == BackendTigris {
		os.Setenv("AWS_REGION", "auto")
	}
}

func minioBucketURL(bucket, endpoint string) string {
	region := envOr("AWS_REGION", "us-east-1")
	ep := endpoint
	if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
		ep = "http://" + ep
	}
	return fmt.Sprintf("s3://%s?endpoint=%s&region=%s&use_path_style=true", bucket, ep, region)
}

func tigrisBucketURL(bucket string) string {
	region := envOr("AWS_REGION", "auto")
	return fmt.Sprintf("s3://%s?region=%s", bucket, region)
}

func cacheDir(cfg StoreConfig, tenantID string) string {
	return filepath.Join(cfg.CacheRoot, tenantPrefix(tenantID, cfg.PrefixRoot))
}

// AgencyCacheDir returns the on-disk cache path for an agency prefix.
func AgencyCacheDir(cfg StoreConfig, agencyID string) string {
	return cacheDir(cfg, agencyID)
}
