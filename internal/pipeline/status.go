package pipeline

import (
	"context"
	"fmt"

	"github.com/leow/go-gedung-peristiwa/internal/model"
)

// BackendStatus is a read-only connectivity snapshot for the dev dashboard.
type BackendStatus struct {
	Backend      Backend
	OK           bool
	Error        string
	Bucket       string
	Endpoint     string
	TotalKeys    int
	KeysByTenant map[string]int
}

// Probe opens each tenant prefix and counts keys (read-only, no writer).
func Probe(ctx context.Context, cfg StoreConfig) BackendStatus {
	st := BackendStatus{
		Backend:      cfg.Backend,
		KeysByTenant: make(map[string]int),
	}
	switch cfg.Backend {
	case BackendMinIO:
		st.Bucket = cfg.MinBucket
		st.Endpoint = cfg.MinEndpoint
	case BackendTigris:
		st.Bucket = cfg.TigrisBucket
		st.Endpoint = "tigris (s3 api)"
	default:
		st.Bucket = "memory"
	}

	if cfg.Backend == BackendMinIO || cfg.Backend == BackendTigris {
		if err := ensureBucket(ctx, cfg); err != nil {
			st.OK = false
			st.Error = fmt.Sprintf("bucket setup: %v", err)
			return st
		}
	}

	for _, id := range AllTenantIDs {
		n, err := probeTenantKeyCount(ctx, cfg, id)
		if err != nil {
			st.OK = false
			if isBucketMissingErr(err) {
				st.Error = fmt.Sprintf("bucket %q not found — run mise run minio-setup or check MINIO_BUCKET", st.Bucket)
			} else {
				st.Error = fmt.Sprintf("tenant %s: %v", id, err)
			}
			return st
		}
		st.KeysByTenant[id] = n
		st.TotalKeys += n
	}
	st.OK = true
	return st
}

func probeTenantKeyCount(ctx context.Context, cfg StoreConfig, tenantID string) (int, error) {
	_, reader, closer, err := OpenReaderDB(ctx, cfg, tenantID)
	if err != nil {
		return 0, err
	}
	defer closer()

	minKey := model.TenantPrefix(tenantID)
	maxKey := model.TenantUpperBound(tenantID)
	keys, err := ScanKeysIter(ctx, reader, minKey, maxKey)
	return len(keys), err
}
