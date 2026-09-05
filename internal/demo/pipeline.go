package demo

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ankur-anand/isledb"

	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

const flushInterval = 5 * time.Second
const ingestCap = 10

// Live-map freshness windows.
const (
	// LiveMapFreshWindow is the age below which a vehicle is drawn normally.
	LiveMapFreshWindow = 5 * time.Minute
	// LiveMapVisibleWindow is the maximum age a vehicle stays on the live map.
	// Vehicles older than this are hidden (but kept in storage for replay).
	LiveMapVisibleWindow = 30 * time.Minute
)

// IngestRecord is one position written to IsleDB (debug ring buffer).
type IngestRecord struct {
	Agency      string
	BucketID    string
	BucketLabel string
	VehicleID   string
	Lat         float64
	Lng         float64
	At          time.Time
}

type agencyWriter struct {
	*pipeline.PrefixDB
}

// Pipeline coordinates IsleDB writers and change-feed readers for all agencies.
type Pipeline struct {
	agencies map[string]*agencyWriter
	cfg      pipeline.StoreConfig

	mu          sync.RWMutex
	eventCount  int64
	lastPoll    time.Time
	vehicleSeen map[string]gtfs.VehiclePosition // latest per vehicle key agency:vehicle
	ingestBuf   []IngestRecord
	lastPolled  []string

	notifyMu sync.Mutex
	pollSubs []chan struct{}
}

// NewPipeline opens one IsleDB writer per agency prefix and hydrates live state from the change feed.
func NewPipeline(ctx context.Context, cfg pipeline.StoreConfig, feeds []gtfs.Feed) (*Pipeline, error) {
	p := &Pipeline{
		agencies:    make(map[string]*agencyWriter),
		cfg:         cfg,
		vehicleSeen: make(map[string]gtfs.VehiclePosition),
	}
	for _, feed := range feeds {
		aw, err := openAgency(ctx, cfg, feed.Agency)
		if err != nil {
			p.Close(ctx)
			return nil, err
		}
		p.agencies[feed.Agency] = aw
	}
	if err := p.hydrateFromSnapshot(ctx); err != nil {
		p.Close(ctx)
		return nil, err
	}
	return p, nil
}

func openAgency(ctx context.Context, cfg pipeline.StoreConfig, agencyID string) (*agencyWriter, error) {
	pdb, err := pipeline.OpenPrefixDB(ctx, pipeline.PrefixOpenConfig{
		Store:      cfg,
		PrefixID:   agencyID,
		FlushEvery: flushInterval,
		Retention:  cfg.Backend != pipeline.BackendMemory,
		RunMaint:   cfg.Backend != pipeline.BackendMemory,
		MaintCtx:   ctx,
	})
	if err != nil {
		return nil, fmt.Errorf("open agency %s: %w", agencyID, err)
	}
	return &agencyWriter{PrefixDB: pdb}, nil
}

// hydrateFromSnapshot seeds vehicleSeen from each agency's current KV state
// via Reader.BootstrapView, rather than replaying the full change feed from
// Oldest. BootstrapView binds the KV snapshot to the exact change-feed
// boundary in one atomic call, which is the documented-safe way to do this
// (calling Snapshot() and ChangeReader.Bounds() separately can race with a
// concurrent writer and skip a committed change) — see IsleDB Learnings in
// AGENTS.md. We don't currently resume the change feed from the returned
// cursor: live updates flow through Write() in this same process, not a
// replayed feed.
func (p *Pipeline) hydrateFromSnapshot(ctx context.Context) error {
	for agencyID, aw := range p.agencies {
		latest, err := aw.bootstrapLatest(ctx)
		if err != nil {
			return fmt.Errorf("hydrate %s: %w", agencyID, err)
		}
		p.mu.Lock()
		for _, pos := range latest {
			key := pos.Agency + ":" + pos.VehicleID
			if prev, ok := p.vehicleSeen[key]; !ok || pos.Timestamp.After(prev.Timestamp) {
				p.vehicleSeen[key] = pos
			}
		}
		p.mu.Unlock()
	}
	return nil
}

// bootstrapLatest returns the latest position per vehicle for this agency
// from an atomic KV+change-feed-cursor snapshot.
func (aw *agencyWriter) bootstrapLatest(ctx context.Context) (map[string]gtfs.VehiclePosition, error) {
	view, err := aw.Reader.BootstrapView(ctx)
	if err != nil {
		return nil, err
	}
	defer view.Snapshot.Close()

	minKey := gtfs.AgencyPrefix(aw.ID)
	maxKey := gtfs.AgencyUpperBound(aw.ID)
	iter, err := view.Snapshot.NewIterator(ctx, isledb.IteratorOptions{
		MinKey: minKey,
		MaxKey: maxKey,
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	latest := make(map[string]gtfs.VehiclePosition)
	for iter.Next() {
		pos, err := gtfs.ParseVehiclePosition(iter.Value())
		if err != nil {
			continue
		}
		vid := gtfs.VehicleIDFromKey(string(iter.Key()))
		if prev, ok := latest[vid]; !ok || pos.Timestamp.After(prev.Timestamp) {
			latest[vid] = pos
		}
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return latest, nil
}

// Write persists vehicle positions grouped by agency.
func (p *Pipeline) Write(ctx context.Context, positions []gtfs.VehiclePosition) (int, error) {
	puts := 0
	for _, pos := range positions {
		aw, ok := p.agencies[pos.Agency]
		if !ok {
			return puts, fmt.Errorf("unknown agency %q", pos.Agency)
		}
		val, err := pos.ValueBytes()
		if err != nil {
			return puts, err
		}
		if err := aw.Writer.Put(ctx, pos.KeyBytes(), val); err != nil {
			return puts, err
		}
		puts++

		p.mu.Lock()
		p.eventCount++
		key := pos.Agency + ":" + pos.VehicleID
		if prev, ok := p.vehicleSeen[key]; !ok || pos.Timestamp.After(prev.Timestamp) {
			p.vehicleSeen[key] = pos
		}
		p.recordIngest(pos)
		p.mu.Unlock()
	}
	return puts, nil
}

// LatestPositions returns the in-memory latest position per vehicle, with no
// freshness filter. Analytics/debug only — use LivePositionsFor for the map.
func (p *Pipeline) LatestPositions() []gtfs.VehiclePosition {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]gtfs.VehiclePosition, 0, len(p.vehicleSeen))
	for _, pos := range p.vehicleSeen {
		out = append(out, pos)
	}
	return out
}

// NotifyPoll signals SSE subscribers that a new poll was ingested.
func (p *Pipeline) NotifyPoll() {
	p.notifyMu.Lock()
	subs := append([]chan struct{}(nil), p.pollSubs...)
	p.notifyMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// SubscribePolls returns a channel notified after each successful GTFS poll.
func (p *Pipeline) SubscribePolls(ctx context.Context) <-chan struct{} {
	ch := make(chan struct{}, 1)
	p.notifyMu.Lock()
	p.pollSubs = append(p.pollSubs, ch)
	p.notifyMu.Unlock()

	go func() {
		<-ctx.Done()
		p.notifyMu.Lock()
		for i, sub := range p.pollSubs {
			if sub == ch {
				p.pollSubs = append(p.pollSubs[:i], p.pollSubs[i+1:]...)
				break
			}
		}
		p.notifyMu.Unlock()
		close(ch)
	}()
	return ch
}

// SetLastPoll records the most recent successful poll time.
func (p *Pipeline) SetLastPoll(t time.Time) {
	p.mu.Lock()
	p.lastPoll = t
	p.mu.Unlock()
}

// Stats returns current vehicle count, last poll time, and total events written.
func (p *Pipeline) Stats() (vehicleCount int, lastPoll time.Time, eventCount int64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.vehicleSeen), p.lastPoll, p.eventCount
}

func (p *Pipeline) recordIngest(pos gtfs.VehiclePosition) {
	bucketID, bucketLabel := "", "Unknown"
	if r, ok := gtfs.RegionForAgency(pos.Agency); ok {
		bucketID = r.ID
		bucketLabel = r.Label
	}
	rec := IngestRecord{
		Agency:      pos.Agency,
		BucketID:    bucketID,
		BucketLabel: bucketLabel,
		VehicleID:   pos.VehicleID,
		Lat:         pos.Lat,
		Lng:         pos.Lng,
		At:          pos.Timestamp,
	}
	p.ingestBuf = append([]IngestRecord{rec}, p.ingestBuf...)
	if len(p.ingestBuf) > ingestCap {
		p.ingestBuf = p.ingestBuf[:ingestCap]
	}
}

// SetLastPolledAgencies records which agencies were polled in the latest cycle.
func (p *Pipeline) SetLastPolledAgencies(agencies []string) {
	p.mu.Lock()
	p.lastPolled = append([]string(nil), agencies...)
	p.mu.Unlock()
}

// LastPolledAgencies returns agencies from the most recent poll cycle.
func (p *Pipeline) LastPolledAgencies() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]string, len(p.lastPolled))
	copy(out, p.lastPolled)
	return out
}

// RecentIngestGrouped returns the ingest ring buffer grouped by bucket label (newest first per group).
func (p *Pipeline) RecentIngestGrouped() map[string][]IngestRecord {
	p.mu.RLock()
	defer p.mu.RUnlock()
	groups := make(map[string][]IngestRecord)
	for _, rec := range p.ingestBuf {
		groups[rec.BucketLabel] = append(groups[rec.BucketLabel], rec)
	}
	return groups
}

// RecentIngestForRegion returns recent ingest records for one region bucket (newest first).
func (p *Pipeline) RecentIngestForRegion(regionID string) []IngestRecord {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]IngestRecord, 0, ingestCap)
	for _, rec := range p.ingestBuf {
		if rec.BucketID == regionID {
			out = append(out, rec)
		}
	}
	return out
}

// LatestPositionsFor returns latest positions filtered to the given agencies,
// with no freshness filter. Analytics/debug only — the live map must use
// LivePositionsFor so vehicles past LiveMapVisibleWindow are hidden.
func (p *Pipeline) LatestPositionsFor(agencies map[string]struct{}) []gtfs.VehiclePosition {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]gtfs.VehiclePosition, 0)
	for _, pos := range p.vehicleSeen {
		if _, ok := agencies[pos.Agency]; ok {
			out = append(out, pos)
		}
	}
	return out
}

// LivePositionsFor returns positions visible on the live map for the given agencies.
// Vehicles older than LiveMapVisibleWindow are excluded (they remain in storage for replay).
func (p *Pipeline) LivePositionsFor(agencies map[string]struct{}, now time.Time) []gtfs.VehiclePosition {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]gtfs.VehiclePosition, 0)
	for _, pos := range p.vehicleSeen {
		if _, ok := agencies[pos.Agency]; !ok {
			continue
		}
		if now.Sub(pos.Timestamp) <= LiveMapVisibleWindow {
			out = append(out, pos)
		}
	}
	return out
}

// StatsFor returns the currently visible vehicle count for filtered agencies,
// plus the last poll time and total events written.
func (p *Pipeline) StatsFor(agencies map[string]struct{}, now time.Time) (vehicleCount int, lastPoll time.Time, eventCount int64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, pos := range p.vehicleSeen {
		if _, ok := agencies[pos.Agency]; !ok {
			continue
		}
		if now.Sub(pos.Timestamp) <= LiveMapVisibleWindow {
			vehicleCount++
		}
	}
	return vehicleCount, p.lastPoll, p.eventCount
}

// ScanLatest returns the latest position per vehicle across all agencies from durable storage.
func (p *Pipeline) ScanLatest(ctx context.Context) ([]gtfs.VehiclePosition, error) {
	latest := make(map[string]gtfs.VehiclePosition)
	for agencyID, aw := range p.agencies {
		positions, err := aw.scanLatest(ctx)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", agencyID, err)
		}
		for _, pos := range positions {
			key := pos.Agency + ":" + pos.VehicleID
			if prev, ok := latest[key]; !ok || pos.Timestamp.After(prev.Timestamp) {
				latest[key] = pos
			}
		}
	}
	out := make([]gtfs.VehiclePosition, 0, len(latest))
	for _, pos := range latest {
		out = append(out, pos)
	}
	return out, nil
}

func (aw *agencyWriter) scanLatest(ctx context.Context) ([]gtfs.VehiclePosition, error) {
	if err := pipeline.RefreshReader(ctx, aw.Reader); err != nil {
		return nil, err
	}
	minKey := gtfs.AgencyPrefix(aw.ID)
	maxKey := gtfs.AgencyUpperBound(aw.ID)
	iter, err := aw.Reader.NewIterator(ctx, isledb.IteratorOptions{
		MinKey: minKey,
		MaxKey: maxKey,
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	latest := make(map[string]gtfs.VehiclePosition)
	for iter.Next() {
		pos, err := gtfs.ParseVehiclePosition(iter.Value())
		if err != nil {
			continue
		}
		vid := gtfs.VehicleIDFromKey(string(iter.Key()))
		if prev, ok := latest[vid]; !ok || pos.Timestamp.After(prev.Timestamp) {
			latest[vid] = pos
		}
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	out := make([]gtfs.VehiclePosition, 0, len(latest))
	for _, pos := range latest {
		out = append(out, pos)
	}
	return out, nil
}

// FlushAll flushes all agency writers and refreshes readers.
func (p *Pipeline) FlushAll(ctx context.Context) error {
	for _, aw := range p.agencies {
		if err := aw.Writer.Flush(ctx); err != nil {
			return err
		}
		if err := pipeline.RefreshReader(ctx, aw.Reader); err != nil {
			return err
		}
	}
	return nil
}

// Close shuts down all agency writers.
func (p *Pipeline) Close(ctx context.Context) error {
	var first error
	for _, aw := range p.agencies {
		if err := aw.Close(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}
