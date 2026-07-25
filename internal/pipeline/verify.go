package pipeline

import (
	"context"
	"fmt"
	"sort"

	"github.com/leow/go-gedung-peristiwa/internal/eventgen"
	"github.com/leow/go-gedung-peristiwa/internal/model"
)

// Result captures simulation write/read metrics.
type Result struct {
	PutCount           int
	UniqueKeysExpected int
	KeysByTenant       map[string][]string
	TailCountByTenant  map[string]int
}

// Verify checks PRD success criteria against expected first-emission keys.
func (p *Pipeline) Verify(ctx context.Context, emissions []eventgen.Emission) (Result, error) {
	expected := eventgen.ExpectedKeys(emissions)
	expectedByTenant := groupKeysByTenant(expected)

	res := Result{
		PutCount:           len(emissions),
		UniqueKeysExpected: len(expected),
		KeysByTenant:       make(map[string][]string),
		TailCountByTenant:  make(map[string]int),
	}

	for tenantID, wantKeys := range expectedByTenant {
		tp, err := p.tenant(tenantID)
		if err != nil {
			return res, err
		}

		gotKeys, err := tp.ScanKeys(ctx)
		if err != nil {
			return res, fmt.Errorf("scan %s: %w", tenantID, err)
		}
		res.KeysByTenant[tenantID] = gotKeys

		if err := verifyKeyOrder(gotKeys); err != nil {
			return res, fmt.Errorf("tenant %s: %w", tenantID, err)
		}

		if err := verifyKeySet(wantKeys, gotKeys); err != nil {
			return res, fmt.Errorf("tenant %s: %w", tenantID, err)
		}

		tailN, err := tp.TailCatchUp(ctx)
		if err != nil {
			return res, fmt.Errorf("tail %s: %w", tenantID, err)
		}
		res.TailCountByTenant[tenantID] = tailN
		if tailN < len(wantKeys) {
			return res, fmt.Errorf("tenant %s: tail saw %d keys, want >= %d", tenantID, tailN, len(wantKeys))
		}
	}

	if res.PutCount < res.UniqueKeysExpected {
		return res, fmt.Errorf("put count %d less than unique events %d", res.PutCount, res.UniqueKeysExpected)
	}

	hasRetry := false
	for _, em := range emissions {
		if em.Retry {
			hasRetry = true
			break
		}
	}
	if hasRetry && res.PutCount <= res.UniqueKeysExpected {
		return res, fmt.Errorf("put count %d should exceed unique events %d when duplicates exist", res.PutCount, res.UniqueKeysExpected)
	}

	return res, nil
}

func groupKeysByTenant(keys []string) map[string][]string {
	out := make(map[string][]string)
	for _, k := range keys {
		tenant := model.TenantFromKey(k)
		out[tenant] = append(out[tenant], k)
	}
	for t := range out {
		sort.Strings(out[t])
	}
	return out
}

func verifyKeyOrder(keys []string) error {
	for i := 1; i < len(keys); i++ {
		if keys[i] < keys[i-1] {
			return fmt.Errorf("key order violation: %q before %q", keys[i-1], keys[i])
		}
	}
	return nil
}

func verifyKeySet(want, got []string) error {
	if len(want) != len(got) {
		return fmt.Errorf("key count got %d want %d", len(got), len(want))
	}
	ws := append([]string(nil), want...)
	gs := append([]string(nil), got...)
	sort.Strings(ws)
	sort.Strings(gs)
	for i := range ws {
		if ws[i] != gs[i] {
			return fmt.Errorf("missing or extra key: got %q want %q", gs[i], ws[i])
		}
	}
	return nil
}
