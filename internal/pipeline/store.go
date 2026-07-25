package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ankur-anand/isledb/blobstore"
)

type Backend string

const (
	BackendMemory Backend = "memory"
	BackendMinIO  Backend = "minio"
	BackendTigris Backend = "tigris"
)

// StoreConfig configures object storage backends.
type StoreConfig struct {
	Backend     Backend
	PrefixRoot  string // optional suffix appended to tenant prefix (e.g. run id)
	CacheRoot   string
	MinEndpoint string
	MinBucket   string
	TigrisBucket string
}

func StoreConfigFromEnv(backend Backend, prefixSuffix string) StoreConfig {
	cfg := StoreConfig{
		Backend:      backend,
		PrefixRoot:   prefixSuffix,
		CacheRoot:    "tmp/cache",
		MinEndpoint:  envOr("MINIO_ENDPOINT", "localhost:9000"),
		MinBucket:    envOr("MINIO_BUCKET", "gedung-peristiwa"),
		TigrisBucket: envOr("TIGRIS_BUCKET", envOr("MINIO_BUCKET", "gedung-peristiwa")),
	}
	return cfg
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

func openStore(ctx context.Context, cfg StoreConfig, tenantID string) (*blobstore.Store, error) {
	prefix := tenantPrefix(tenantID, cfg.PrefixRoot)
	switch cfg.Backend {
	case BackendMemory:
		return blobstore.NewMemory(prefix), nil
	case BackendMinIO:
		url := minioBucketURL(cfg.MinBucket, cfg.MinEndpoint)
		return blobstore.Open(ctx, url, prefix)
	case BackendTigris:
		url := tigrisBucketURL(cfg.TigrisBucket)
		return blobstore.Open(ctx, url, prefix)
	default:
		return nil, fmt.Errorf("unknown backend %q", cfg.Backend)
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
