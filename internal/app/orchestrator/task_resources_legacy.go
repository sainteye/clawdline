package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"

	"github.com/sainteye/clawdline/internal/adapters/resourcefs"
	"github.com/sainteye/clawdline/internal/domain/taskresource"
)

// Legacy ownership is counted without reclassifying or adopting any directory.
// Task work roots, their registered gate caches and raw callback logs are
// disjoint from task-resources-v1; cache bytes are subtracted from work once.
func (b *Broker) legacyResourceUsage(ctx context.Context) (taskresource.Usage, error) {
	u := taskresource.Usage{Known: true}
	after := ""
	add := func(dst *int64, n int64) error {
		if n < 0 || *dst > math.MaxInt64-n {
			return errors.New("resource_measurement_unknown")
		}
		*dst += n
		return nil
	}
	for {
		rows, next, err := b.Store.TaskResourceSourcePage(ctx, after)
		if err != nil {
			return u, err
		}
		for _, row := range rows {
			var r Record
			if json.Unmarshal(row.Record, &r) != nil || r.ID != row.ID || !IsTaskID(r.ID) {
				return u, errors.New("resource_source_unknown")
			}
			dir := b.Tasks.Path(r.ID)
			if _, err := os.Lstat(dir); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return u, err
			}
			if !ownedPath(b.Tasks.Dir, r.Dir, r.ID) || filepath.Clean(r.Dir) != filepath.Clean(dir) {
				return u, errors.New("resource_source_path_unverified")
			}
			work := filepath.Join(dir, "work")
			workSize, err := resourcefs.LegacySize(ctx, work)
			if os.IsNotExist(err) {
				workSize = 0
			} else if err != nil {
				return u, err
			}
			var cacheSize int64
			if r.Gate != nil && workSize > 0 {
				caches, err := b.Store.GoCachesByOwner(ctx, "task", r.ID)
				if err != nil {
					return u, err
				}
				for _, cache := range caches {
					if cache.Path != b.gateScratch(r.ID).Cache || !cache.Rebuildable {
						return u, errors.New("resource_source_cache_unverified")
					}
					n, err := resourcefs.LegacySize(ctx, cache.Path)
					if os.IsNotExist(err) {
						continue
					} else if err != nil {
						return u, err
					}
					if err := add(&cacheSize, n); err != nil {
						return u, err
					}
					if err := add(&u.CacheTotal, n); err != nil {
						return u, err
					}
					u.LargestCache = max(u.LargestCache, n)
				}
			}
			if cacheSize > workSize {
				return u, errors.New("resource_source_changed_during_measurement")
			}
			if err := add(&u.ScratchTotal, workSize-cacheSize); err != nil {
				return u, err
			}
			if r.Callback != nil {
				n, err := resourcefs.LegacySize(ctx, (callbackFiles{dir}).log())
				if os.IsNotExist(err) {
					continue
				} else if err != nil {
					return u, err
				}
				if err := add(&u.ScratchTotal, n); err != nil {
					return u, err
				}
			}
		}
		if next == "" {
			return u, nil
		}
		after = next
	}
}

func (b *Broker) taskPreservationDigest(ctx context.Context, id string) (string, error) {
	o, err := b.resourceOwner(ctx, "task", id)
	if err != nil || !o.Complete || !o.OutputsSaved {
		return "", errors.New("outputs_unverified")
	}
	files, err := resourcefs.Manifest(ctx, o.OutputRoot)
	if err != nil {
		return "", err
	}
	return evidenceDigest(struct{ Files, Evidence string }{files.Digest, o.EvidenceDigest}), nil
}
