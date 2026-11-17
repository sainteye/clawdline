package projectassistanthost

import (
	"context"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	"testing"
)

type heldConfigurationProvider struct {
	app.Provider
	entered chan struct{}
	release chan struct{}
}

func (p heldConfigurationProvider) Configuration(ctx context.Context, b pa.Bot, dir, model string) (pa.ConfigurationCatalog, error) {
	close(p.entered)
	select {
	case <-p.release:
		return pa.ConfigurationCatalog{Agent: b.Agent}, nil
	case <-ctx.Done():
		return pa.ConfigurationCatalog{}, ctx.Err()
	}
}

func TestConfigurationDiscoveryRefusesConcurrentWaiters(t *testing.T) {
	p := heldConfigurationProvider{entered: make(chan struct{}), release: make(chan struct{})}
	h := &Host{o: Options{Provider: func() (app.Provider, error) { return p, nil }}, configurationSlots: make(chan struct{}, pa.MaxConfigurationProbes)}
	done := make(chan error, 1)
	go func() {
		_, err := h.configuration(context.Background(), pa.Bot{Agent: pa.Codex}, "synthetic", " ")
		done <- err
	}()
	<-p.entered
	if h.ConfigurationUsage() != 1 {
		t.Fatal("active discovery is not measured")
	}
	if _, err := h.configuration(context.Background(), pa.Bot{}, "synthetic", ""); pa.Code(err) != pa.CapacityFull {
		t.Fatalf("concurrent discovery queued: %v", err)
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if h.ConfigurationUsage() != 0 {
		t.Fatal("discovery slot leaked")
	}
}
