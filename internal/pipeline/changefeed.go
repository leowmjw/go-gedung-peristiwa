package pipeline

import (
	"context"
	"fmt"

	"github.com/ankur-anand/isledb"
)

// CountChangeFeed returns the number of committed changes from Oldest through Head.
func CountChangeFeed(ctx context.Context, db *isledb.DB) (int, error) {
	cr, err := db.OpenChangeReader(ctx)
	if err != nil {
		return 0, err
	}
	defer cr.Close()

	bounds, err := cr.Bounds(ctx)
	if err != nil {
		return 0, err
	}
	if bounds.Oldest.IsZero() {
		return 0, nil
	}

	opts := isledb.DefaultChangeReadOptions()
	cursor := bounds.Oldest
	total := 0
	for {
		page, err := cr.Read(ctx, cursor, opts)
		if err != nil {
			return total, err
		}
		total += len(page.Changes)
		cursor = page.Next
		if page.CaughtUp() {
			break
		}
	}
	return total, nil
}

// DrainChangeFeed applies every change from Oldest until caught up.
func DrainChangeFeed(ctx context.Context, db *isledb.DB, apply func(isledb.Change) error) (int, error) {
	cr, err := db.OpenChangeReader(ctx)
	if err != nil {
		return 0, err
	}
	defer cr.Close()

	bounds, err := cr.Bounds(ctx)
	if err != nil {
		return 0, err
	}
	if bounds.Oldest.IsZero() {
		return 0, nil
	}

	opts := isledb.DefaultChangeReadOptions()
	cursor := bounds.Oldest
	total := 0
	for {
		page, err := cr.Read(ctx, cursor, opts)
		if err != nil {
			return total, err
		}
		for _, ch := range page.Changes {
			if err := apply(ch); err != nil {
				return total, err
			}
			total++
		}
		cursor = page.Next
		if page.CaughtUp() {
			break
		}
	}
	return total, nil
}

// ChangeFeedSummary describes the durable mutation feed for one prefix.
type ChangeFeedSummary struct {
	ChangeCount int
	From        int64 // earliest vehicle timestamp_ns seen (0 if unknown)
	To          int64 // latest vehicle timestamp_ns seen (0 if unknown)
	Empty       bool
}

// SummarizeChangeFeed scans the feed once and returns coarse catalog metadata.
func SummarizeChangeFeed(ctx context.Context, db *isledb.DB, timestampNS func(key []byte) int64) (ChangeFeedSummary, error) {
	cr, err := db.OpenChangeReader(ctx)
	if err != nil {
		return ChangeFeedSummary{}, err
	}
	defer cr.Close()

	bounds, err := cr.Bounds(ctx)
	if err != nil {
		return ChangeFeedSummary{}, err
	}
	if bounds.Oldest.IsZero() {
		return ChangeFeedSummary{Empty: true}, nil
	}

	opts := isledb.DefaultChangeReadOptions()
	cursor := bounds.Oldest
	var sum ChangeFeedSummary
	for {
		page, err := cr.Read(ctx, cursor, opts)
		if err != nil {
			return sum, err
		}
		for _, ch := range page.Changes {
			if ch.Operation != isledb.ChangePut {
				continue
			}
			sum.ChangeCount++
			if timestampNS == nil {
				continue
			}
			ts := timestampNS(ch.Key)
			if ts <= 0 {
				continue
			}
			if sum.From == 0 || ts < sum.From {
				sum.From = ts
			}
			if ts > sum.To {
				sum.To = ts
			}
		}
		cursor = page.Next
		if page.CaughtUp() {
			break
		}
	}
	sum.Empty = sum.ChangeCount == 0
	return sum, nil
}

// ScanKeysIter collects keys in lexicographic order using a long-lived reader.
func ScanKeysIter(ctx context.Context, reader *isledb.Reader, minKey, maxKey []byte) ([]string, error) {
	if err := reader.Refresh(ctx); err != nil {
		return nil, err
	}
	iter, err := reader.NewIterator(ctx, isledb.IteratorOptions{
		MinKey: minKey,
		MaxKey: maxKey,
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var keys []string
	for iter.Next() {
		keys = append(keys, string(iter.Key()))
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}

// RefreshReader forces the reader to load the latest committed manifest view.
func RefreshReader(ctx context.Context, reader *isledb.Reader) error {
	if reader == nil {
		return fmt.Errorf("nil reader")
	}
	return reader.Refresh(ctx)
}
