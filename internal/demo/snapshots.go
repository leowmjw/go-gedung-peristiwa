package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/ankur-anand/isledb/blobstore"
	isledbmanifest "github.com/ankur-anand/isledb/manifest"

	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
)

// ManifestSnapshot is one durable manifest point in object storage (snapshot file or manifest log entry).
type ManifestSnapshot struct {
	Agency      string    `json:"agency"`
	ID          string    `json:"id"`
	Size        int64     `json:"size"`
	At          time.Time `json:"at"`
	S3Key       string    `json:"s3Key"`
	Source      string    `json:"source"` // "snapshot" or "log"
	WatermarkNS int64     `json:"-"`
}

// AgencyCatalog lists manifest snapshots for one agency (may be empty).
type AgencyCatalog struct {
	Agency    string             `json:"agency"`
	Count     int                `json:"count"`
	Snapshots []ManifestSnapshot `json:"snapshots"`
}

// RegionCatalog is the sparse snapshot inventory for a map region.
type RegionCatalog struct {
	RegionID string          `json:"regionId"`
	Label    string          `json:"label"`
	Empty    bool            `json:"empty"`
	Message  string          `json:"message,omitempty"`
	Total    int             `json:"total"`
	Agencies []AgencyCatalog `json:"agencies"`
	Notes    []string        `json:"notes,omitempty"`
}

// ListManifestSnapshots lists manifest snapshot files and, if none exist, flush events from manifest/log/.
func (p *Pipeline) ListManifestSnapshots(ctx context.Context, agencyID string) ([]ManifestSnapshot, error) {
	aw, ok := p.agencies[agencyID]
	if !ok {
		return nil, fmt.Errorf("unknown agency %q", agencyID)
	}
	return listManifestSnapshots(ctx, aw.store, agencyID)
}

func listManifestSnapshots(ctx context.Context, store *blobstore.Store, agencyID string) ([]ManifestSnapshot, error) {
	out, err := listSnapshotManifestFiles(ctx, store, agencyID)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		out, err = listManifestLogSnapshots(ctx, store, agencyID)
		if err != nil {
			return nil, err
		}
	}
	sortSnapshots(out)
	return out, nil
}

func listSnapshotManifestFiles(ctx context.Context, store *blobstore.Store, agencyID string) ([]ManifestSnapshot, error) {
	result, err := store.List(ctx, blobstore.ListOptions{Prefix: "manifest/snapshots/"})
	if err != nil {
		return nil, err
	}
	out := make([]ManifestSnapshot, 0, len(result.Objects))
	for _, obj := range result.Objects {
		if obj.IsDir || !strings.HasSuffix(obj.Key, ".manifest") {
			continue
		}
		id := snapshotIDFromObjectKey(obj.Key)
		if id == "" {
			continue
		}
		snap := ManifestSnapshot{
			Agency: agencyID,
			ID:     id,
			Size:   obj.Size,
			S3Key:  path.Join("manifest", "snapshots", id+".manifest"),
			Source: "snapshot",
		}
		data, _, err := store.Read(ctx, store.ManifestSnapshotPath(id))
		if err == nil {
			snap.At, snap.WatermarkNS = snapshotTimes(data)
		}
		out = append(out, snap)
	}
	return out, nil
}

func listManifestLogSnapshots(ctx context.Context, store *blobstore.Store, agencyID string) ([]ManifestSnapshot, error) {
	objs, err := store.ListManifestLogs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ManifestSnapshot, 0, len(objs))
	for _, obj := range objs {
		if obj.IsDir || !strings.HasSuffix(obj.Key, ".json") {
			continue
		}
		id := strings.TrimSuffix(path.Base(obj.Key), ".json")
		logKey := store.ManifestLogPath(id)
		data, _, err := store.Read(ctx, logKey)
		if err != nil {
			continue
		}
		entry, err := isledbmanifest.DecodeLogEntry(data)
		if err != nil {
			continue
		}
		switch entry.Op {
		case isledbmanifest.LogOpAddSSTable, isledbmanifest.LogOpCheckpoint:
		default:
			continue
		}
		at := entry.Timestamp
		wm := int64(0)
		if entry.SSTable != nil {
			if entry.SSTable.CreatedAt.After(at) {
				at = entry.SSTable.CreatedAt
			}
			if len(entry.SSTable.MaxKey) > 0 {
				wm = gtfs.TimestampNSFromKey(string(entry.SSTable.MaxKey))
			}
		}
		if entry.Checkpoint != nil {
			_, ckWM := snapshotTimesFromManifest(entry.Checkpoint)
			if ckWM > wm {
				wm = ckWM
			}
		}
		if wm <= 0 {
			wm = at.UnixNano()
		}
		if at.IsZero() {
			at = time.Unix(0, wm)
		}
		out = append(out, ManifestSnapshot{
			Agency:      agencyID,
			ID:          id,
			Size:        obj.Size,
			At:          at,
			S3Key:       path.Join("manifest", "log", id+".json"),
			Source:      "log",
			WatermarkNS: wm,
		})
	}
	return out, nil
}

func sortSnapshots(out []ManifestSnapshot) {
	sort.Slice(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.After(out[j].At)
		}
		return out[i].ID > out[j].ID
	})
}

func snapshotIDFromObjectKey(key string) string {
	base := path.Base(key)
	return strings.TrimSuffix(base, ".manifest")
}

func snapshotTimes(data []byte) (time.Time, int64) {
	var m isledbmanifest.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return time.Time{}, 0
	}
	return snapshotTimesFromManifest(&m)
}

func snapshotTimesFromManifest(m *isledbmanifest.Manifest) (time.Time, int64) {
	var maxAt time.Time
	var maxWM int64
	walkSST := func(s isledbmanifest.SSTMeta) {
		if s.CreatedAt.After(maxAt) {
			maxAt = s.CreatedAt
		}
		if len(s.MaxKey) > 0 {
			if wm := gtfs.TimestampNSFromKey(string(s.MaxKey)); wm > maxWM {
				maxWM = wm
			}
		}
	}
	for _, s := range m.L0SSTs {
		walkSST(s)
	}
	for _, run := range m.SortedRuns {
		for _, s := range run.SSTs {
			walkSST(s)
		}
	}
	if maxAt.IsZero() && m.WriterFence != nil {
		maxAt = m.WriterFence.ClaimedAt
	}
	if maxWM <= 0 && !maxAt.IsZero() {
		maxWM = maxAt.UnixNano()
	}
	return maxAt, maxWM
}

// CatalogForRegion returns manifest snapshot inventory for all agencies in a region.
func (p *Pipeline) CatalogForRegion(ctx context.Context, regionID string) (RegionCatalog, error) {
	rgn, err := gtfs.RegionByID(regionID)
	if err != nil {
		return RegionCatalog{}, err
	}
	cat := RegionCatalog{
		RegionID: rgn.ID,
		Label:    rgn.Label,
		Notes: []string{
			"Catalog lists IsleDB manifest snapshots (manifest/snapshots) or manifest log flush events when snapshots are absent.",
			"Playback uses key timestamps up to each snapshot watermark (not a pinned manifest reader).",
		},
	}
	for _, agencyID := range rgn.Agencies {
		aw, ok := p.agencies[agencyID]
		if !ok {
			cat.Agencies = append(cat.Agencies, AgencyCatalog{Agency: agencyID, Count: 0})
			continue
		}
		snaps, err := listManifestSnapshots(ctx, aw.store, agencyID)
		if err != nil {
			return RegionCatalog{}, fmt.Errorf("agency %s: %w", agencyID, err)
		}
		cat.Agencies = append(cat.Agencies, AgencyCatalog{
			Agency:    agencyID,
			Count:     len(snaps),
			Snapshots: snaps,
		})
		cat.Total += len(snaps)
	}
	if cat.Total == 0 {
		cat.Empty = true
		cat.Message = fmt.Sprintf(
			"No manifest snapshots in storage for %s. Poll this region on the live map and wait for writer flush (~5s).",
			rgn.Label,
		)
	}
	return cat, nil
}
