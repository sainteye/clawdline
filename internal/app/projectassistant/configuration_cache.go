package projectassistant

import (
	"sync"
	"time"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// configurationKey names everything a catalog read hands the native probe:
// the Agent, the login profile whose account answers, the requested model
// and the Project directory whose settings the Agent loads. Saved option
// choices are cleared before a read, so they are deliberately absent.
type configurationKey struct {
	Agent        pa.Agent
	LoginProfile string
	Model        string
	Directory    string
}

type configurationEntry struct {
	catalog pa.ConfigurationCatalog
	at      time.Time
}

// configurationCache keeps the last catalog the native Agent reported, so
// reopening the model dialog does not start a disposable Agent process each
// time (measured 1.0–2.7 s per open on 2026-11-23). It is not a second source
// of truth: every entry is a verbatim native answer, it expires after
// pa.MaxConfigurationCacheSeconds, and a save still validates the chosen
// model and options in its own native session, which drops the entries of
// that account and directory when the Agent no longer offers them.
type configurationCache struct {
	mu      sync.Mutex
	entries map[configurationKey]configurationEntry
}

func newConfigurationCache() *configurationCache {
	return &configurationCache{entries: map[configurationKey]configurationEntry{}}
}

func (c *configurationCache) get(k configurationKey, now time.Time) (pa.ConfigurationCatalog, bool) {
	if c == nil {
		return pa.ConfigurationCatalog{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	if !ok {
		return pa.ConfigurationCatalog{}, false
	}
	age := now.Sub(e.at)
	if age < 0 || age >= time.Duration(pa.MaxConfigurationCacheSeconds)*time.Second {
		delete(c.entries, k)
		return pa.ConfigurationCatalog{}, false
	}
	return cloneCatalog(e.catalog), true
}

func (c *configurationCache) put(k configurationKey, catalog pa.ConfigurationCatalog, now time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[k]; !ok && len(c.entries) >= pa.MaxConfigurationCacheEntries {
		var oldest configurationKey
		first := true
		for key, e := range c.entries {
			if first || e.at.Before(c.entries[oldest].at) {
				oldest, first = key, false
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[k] = configurationEntry{catalog: cloneCatalog(catalog), at: now}
}

// forget drops every model of one Agent account in one directory.
func (c *configurationCache) forget(agent pa.Agent, profile, directory string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.entries {
		if k.Agent == agent && k.LoginProfile == profile && k.Directory == directory {
			delete(c.entries, k)
		}
	}
}

// ConfigurationCacheUsage reports retained catalogs and the age of the oldest
// catalog still eligible to serve a request. Expired entries are never reused.
func (s *Service) ConfigurationCacheUsage() (entries int, oldestSeconds int64) {
	c := s.catalogs
	if c == nil {
		return 0, 0
	}
	now := s.opts.Clock()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.entries {
		age := now.Sub(e.at)
		if age >= 0 && age < time.Duration(pa.MaxConfigurationCacheSeconds)*time.Second {
			oldestSeconds = max(oldestSeconds, int64(age/time.Second))
		}
	}
	return len(c.entries), oldestSeconds
}

func cloneCatalog(in pa.ConfigurationCatalog) pa.ConfigurationCatalog {
	out := in
	if in.Options == nil {
		return out
	}
	out.Options = make([]pa.ConfigOption, len(in.Options))
	for i, o := range in.Options {
		if o.Choices != nil {
			o.Choices = append(make([]pa.ConfigChoice, 0, len(o.Choices)), o.Choices...)
		}
		out.Options[i] = o
	}
	return out
}
