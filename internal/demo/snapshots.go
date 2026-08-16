package demo

import (
	"context"
	"fmt"
	"time"

	"github.com/leow/go-gedung-peristiwa/internal/gtfs"
	"github.com/leow/go-gedung-peristiwa/internal/pipeline"
)

// FeedEntry is one catalog row derived from the durable change feed.
type FeedEntry struct {
	Agency      string    `json:"agency"`
	ChangeCount int       `json:"changeCount"`
	From        time.Time `json:"from"`
	To          time.Time `json:"to"`
}

// AgencyCatalog lists change-feed metadata for one agency (may be empty).
type AgencyCatalog struct {
	Agency  string      `json:"agency"`
	Count   int         `json:"count"`
	Entries []FeedEntry `json:"entries"`
}

// RegionCatalog is the sparse change-feed inventory for a map region.
type RegionCatalog struct {
	RegionID string          `json:"regionId"`
	Label    string          `json:"label"`
	Empty    bool            `json:"empty"`
	Message  string          `json:"message,omitempty"`
	Total    int             `json:"total"`
	Agencies []AgencyCatalog `json:"agencies"`
	Notes    []string        `json:"notes,omitempty"`
}

func agencyTimestampNS(key []byte) int64 {
	return gtfs.TimestampNSFromKey(string(key))
}

func (p *Pipeline) summarizeAgencyFeed(ctx context.Context, aw *agencyWriter) (FeedEntry, error) {
	sum, err := pipeline.SummarizeChangeFeed(ctx, aw.DB, agencyTimestampNS)
	if err != nil {
		return FeedEntry{}, err
	}
	entry := FeedEntry{
		Agency:      aw.ID,
		ChangeCount: sum.ChangeCount,
	}
	if sum.From > 0 {
		entry.From = time.Unix(0, sum.From)
	}
	if sum.To > 0 {
		entry.To = time.Unix(0, sum.To)
	}
	return entry, nil
}

// CatalogForRegion returns change-feed inventory for all agencies in a region.
func (p *Pipeline) CatalogForRegion(ctx context.Context, regionID string) (RegionCatalog, error) {
	rgn, err := gtfs.RegionByID(regionID)
	if err != nil {
		return RegionCatalog{}, err
	}
	cat := RegionCatalog{
		RegionID: rgn.ID,
		Label:    rgn.Label,
		Notes: []string{
			"Catalog summarizes IsleDB change-feed history (full PUT values).",
			"Playback walks committed mutations in writer order and dedupes to latest per vehicle.",
		},
	}
	for _, agencyID := range rgn.Agencies {
		aw, ok := p.agencies[agencyID]
		if !ok {
			cat.Agencies = append(cat.Agencies, AgencyCatalog{Agency: agencyID, Count: 0})
			continue
		}
		entry, err := p.summarizeAgencyFeed(ctx, aw)
		if err != nil {
			return RegionCatalog{}, fmt.Errorf("agency %s: %w", agencyID, err)
		}
		cat.Agencies = append(cat.Agencies, AgencyCatalog{
			Agency:  agencyID,
			Count:   entry.ChangeCount,
			Entries: []FeedEntry{entry},
		})
		cat.Total += entry.ChangeCount
	}
	if cat.Total == 0 {
		cat.Empty = true
		cat.Message = fmt.Sprintf(
			"No change-feed history for %s. Poll this region on the live map and wait for writer flush (~5s).",
			rgn.Label,
		)
	}
	return cat, nil
}
