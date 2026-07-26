package demo

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ankur-anand/isledb"
	"github.com/ankur-anand/isledb/blobstore"

	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

const flushInterval = 5 * time.Second
const ingestCap = 10

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

// Update is a vehicle position streamed from the tailing reader.
type Update struct {
	Position gtfs.VehiclePosition
}

type agencyWriter struct {
	id        string
	store     *blobstore.Store
	db        *isledb.DB
	writer    *isledb.Writer
	compactor *isledb.Compactor
	cacheDir  string
}

// Pipeline coordinates IsleDB writers and tailing readers for all agencies.
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

// NewPipeline opens one IsleDB writer per agency prefix.
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
	return p, nil
}

func openAgency(ctx context.Context, cfg pipeline.StoreConfig, agencyID string) (*agencyWriter, error) {
	store, err := pipeline.OpenAgencyStore(ctx, cfg, agencyID)
	if err != nil {
		return nil, fmt.Errorf("open store %s: %w", agencyID, err)
	}

	db, err := isledb.OpenDB(ctx, store, isledb.DBOptions{})
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("open db %s: %w", agencyID, err)
	}

	wOpts := isledb.DefaultWriterOptions()
	wOpts.FlushInterval = flushInterval
	writer, err := db.OpenWriter(ctx, wOpts)
	if err != nil {
		db.Close()
		store.Close()
		return nil, fmt.Errorf("open writer %s: %w", agencyID, err)
	}

	cOpts := isledb.DefaultCompactorOptions()
	cOpts.CheckInterval = 10 * time.Second
	compactor, err := db.OpenCompactor(ctx, cOpts)
	if err != nil {
		writer.Close()
		db.Close()
		store.Close()
		return nil, fmt.Errorf("open compactor %s: %w", agencyID, err)
	}
	compactor.Start()

	cache := pipeline.AgencyCacheDir(cfg, agencyID)
	if err := os.MkdirAll(cache, 0o755); err != nil {
		compactor.Close()
		writer.Close()
		db.Close()
		store.Close()
		return nil, err
	}

	return &agencyWriter{
		id:        agencyID,
		store:     store,
		db:        db,
		writer:    writer,
		compactor: compactor,
		cacheDir:  cache,
	}, nil
}

// Write persists vehicle positions grouped by agency.
func (p *Pipeline) Write(positions []gtfs.VehiclePosition) (int, error) {
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
		if err := aw.writer.Put(pos.KeyBytes(), val); err != nil {
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

// LatestPositions returns the in-memory latest position per vehicle.
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

// LatestPositionsFor returns latest positions filtered to the given agencies.
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

// StatsFor returns vehicle count for filtered agencies, last poll, and total events.
func (p *Pipeline) StatsFor(agencies map[string]struct{}) (vehicleCount int, lastPoll time.Time, eventCount int64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, pos := range p.vehicleSeen {
		if _, ok := agencies[pos.Agency]; ok {
			vehicleCount++
		}
	}
	return vehicleCount, p.lastPoll, p.eventCount
}

// ScanLatest returns the latest position per vehicle across all agencies.
func (p *Pipeline) ScanLatest(ctx context.Context) ([]gtfs.VehiclePosition, error) {
	latest := make(map[string]gtfs.VehiclePosition)
	for agencyID, aw := range p.agencies {
		positions, err := aw.scan(ctx)
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

func (aw *agencyWriter) scan(ctx context.Context) ([]gtfs.VehiclePosition, error) {
	reader, err := isledb.OpenReader(ctx, aw.store, isledb.ReaderOpenOptions{
		CacheDir: aw.cacheDir,
	})
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	if err := reader.Refresh(ctx); err != nil {
		return nil, err
	}

	minKey := gtfs.AgencyPrefix(aw.id)
	maxKey := gtfs.AgencyUpperBound(aw.id)
	rows, err := reader.ScanLimit(ctx, minKey, maxKey, 0)
	if err != nil {
		return nil, err
	}

	latest := make(map[string]gtfs.VehiclePosition)
	for _, kv := range rows {
		pos, err := gtfs.ParseVehiclePosition(kv.Value)
		if err != nil {
			continue
		}
		vid := gtfs.VehicleIDFromKey(string(kv.Key))
		if prev, ok := latest[vid]; !ok || pos.Timestamp.After(prev.Timestamp) {
			latest[vid] = pos
		}
	}
	out := make([]gtfs.VehiclePosition, 0, len(latest))
	for _, pos := range latest {
		out = append(out, pos)
	}
	return out, nil
}

// TailUpdates streams new vehicle positions from all agencies until ctx is cancelled.
func (p *Pipeline) TailUpdates(ctx context.Context) <-chan Update {
	ch := make(chan Update, 256)
	var wg sync.WaitGroup
	for agencyID, aw := range p.agencies {
		wg.Add(1)
		go func(agencyID string, aw *agencyWriter) {
			defer wg.Done()
			aw.tail(ctx, ch)
		}(agencyID, aw)
	}
	go func() {
		wg.Wait()
		close(ch)
	}()
	return ch
}

func (aw *agencyWriter) tail(ctx context.Context, ch chan<- Update) {
	opts := isledb.DefaultTailingReaderOpenOptions()
	opts.ReaderOptions.CacheDir = aw.cacheDir
	tr, err := isledb.OpenTailingReader(ctx, aw.store, opts)
	if err != nil {
		return
	}
	defer tr.Close()
	if err := tr.Start(); err != nil {
		return
	}

	minKey := gtfs.AgencyPrefix(aw.id)
	maxKey := gtfs.AgencyUpperBound(aw.id)
	_ = tr.Tail(ctx, isledb.TailOptions{
		MinKey:       minKey,
		MaxKey:       maxKey,
		PollInterval: 500 * time.Millisecond,
	}, func(kv isledb.KV) error {
		pos, err := gtfs.ParseVehiclePosition(kv.Value)
		if err != nil {
			return nil
		}
		select {
		case ch <- Update{Position: pos}:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
}

// FlushAll flushes all agency writers.
func (p *Pipeline) FlushAll(ctx context.Context) error {
	for _, aw := range p.agencies {
		if err := aw.writer.Flush(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Close shuts down all agency writers.
func (p *Pipeline) Close(ctx context.Context) error {
	var first error
	for _, aw := range p.agencies {
		if err := aw.close(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (aw *agencyWriter) close(ctx context.Context) error {
	if err := aw.writer.Flush(ctx); err != nil {
		return err
	}
	if err := aw.writer.Close(); err != nil {
		return err
	}
	aw.compactor.Stop()
	if err := aw.compactor.Close(); err != nil {
		return err
	}
	if err := aw.db.Close(); err != nil {
		return err
	}
	return aw.store.Close()
}
