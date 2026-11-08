package updater

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/release"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// The signature response releases the manifest response. Sequential reads
// cannot finish; no timing comparison is needed to prove the dependency.
func TestFetchDownloadsMetadataTogether(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	f.publish("v0.11.0", commitOf('b'), []byte("artifact"), true, nil)
	signatureStarted := make(chan struct{})
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if strings.HasSuffix(r.URL.Path, "manifest.sig.json") {
			close(signatureStarted)
		} else {
			select {
			case <-signatureStarted:
			case <-r.Context().Done():
				return
			}
		}
		f.srv.Config.Handler.ServeHTTP(w, r)
	}))
	defer srv.Close()
	f.env.ReleaseURL = srv.URL + "/latest"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	m, err := f.env.Fetch(ctx, f.env.ReleaseURL)
	if err != nil || m.Version != "v0.11.0" {
		t.Fatalf("metadata reads did not overlap into a verified release: %s %v", m.Version, err)
	}
	if requests.Load() != MetadataReadConcurrencyLimit {
		t.Fatalf("metadata requests=%d, want exactly the manifest/signature pair", requests.Load())
	}
}

func TestFetchPreservesMetadataFailurePrecedence(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		manifestStatus, signatureStatus int
		signature                       []byte
		want                            string
	}{
		{name: "no manifest", manifestStatus: 404, signatureStatus: 200, want: CodeNoReleasePublished},
		{name: "manifest failure before missing signature", manifestStatus: 504, signatureStatus: 404, want: CodeReleaseUnreachable},
		{name: "missing signature", manifestStatus: 200, signatureStatus: 404, want: release.CodeManifestUnsigned},
		{name: "invalid signature", manifestStatus: 200, signatureStatus: 200, signature: []byte("not JSON"), want: release.CodeSignatureInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, "v0.10.0")
			f.publish("v0.11.0", commitOf('b'), []byte("artifact"), true, nil)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				status := tc.manifestStatus
				if strings.HasSuffix(r.URL.Path, "manifest.sig.json") {
					status = tc.signatureStatus
					if tc.signature != nil {
						w.WriteHeader(status)
						w.Write(tc.signature)
						return
					}
				}
				if status != 200 {
					w.WriteHeader(status)
					return
				}
				f.srv.Config.Handler.ServeHTTP(w, r)
			}))
			defer srv.Close()
			_, err := f.env.Fetch(context.Background(), srv.URL+"/latest")
			var re *release.Error
			got := errCode(err)
			if errors.As(err, &re) {
				got = re.Code
			}
			if got != tc.want {
				t.Fatalf("error=%v, code=%s want %s", err, got, tc.want)
			}
		})
	}
}

func TestFetchCancelsSiblingAfterManifestFailure(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	started := make(chan struct{})
	cancelled := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "manifest.sig.json") {
			close(started)
			<-r.Context().Done()
			close(cancelled)
			return
		}
		select {
		case <-started:
			w.WriteHeader(404)
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := f.env.Fetch(ctx, srv.URL+"/latest")
	if errCode(err) != CodeNoReleasePublished {
		t.Fatalf("manifest error=%v", err)
	}
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("signature read outlived a failed manifest")
	}
}

func TestFetchCancellationStopsBothDownloads(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	var started, stopped atomic.Int64
	bothStarted := make(chan struct{})
	bothStopped := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if started.Add(1) == 2 {
			close(bothStarted)
		}
		<-r.Context().Done()
		if stopped.Add(1) == 2 {
			close(bothStopped)
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := f.env.Fetch(ctx, srv.URL+"/latest"); done <- err }()
	select {
	case <-bothStarted:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("both metadata reads did not start")
	}
	cancel()
	select {
	case err := <-done:
		if errCode(err) != CodeReleaseUnreachable {
			t.Fatalf("cancellation=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled verification did not return")
	}
	select {
	case <-bothStopped:
	case <-time.After(time.Second):
		t.Fatal("metadata requests outlived cancellation")
	}
}

func TestMetadataConcurrencyOverrideKeepsSequentialVerification(t *testing.T) {
	t.Setenv(capacity.OverrideEnv, capacity.ReleaseMetadataConcurrency+"=1")
	env, err := DefaultEnv(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if env.MetadataConcurrency != 1 {
		t.Fatalf("lowered metadata concurrency=%d", env.MetadataConcurrency)
	}
	f := newFixture(t, "v0.10.0")
	f.publish("v0.11.0", commitOf('b'), []byte("artifact"), true, nil)
	f.env.MetadataConcurrency = env.MetadataConcurrency
	// The signature must see the manifest handler finish before it is sent.
	finished := make(chan struct{})
	var overlap atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "manifest.json") {
			time.Sleep(20 * time.Millisecond)
			f.srv.Config.Handler.ServeHTTP(w, r)
			close(finished)
			return
		}
		select {
		case <-finished:
		default:
			overlap.Store(true)
		}
		f.srv.Config.Handler.ServeHTTP(w, r)
	}))
	defer srv.Close()
	m, err := f.env.Fetch(context.Background(), srv.URL+"/latest")
	if err != nil || m.Version != "v0.11.0" || overlap.Load() {
		t.Fatalf("sequential verified fetch=%s err=%v overlap=%v", m.Version, err, overlap.Load())
	}
	t.Setenv(capacity.OverrideEnv, capacity.ReleaseMetadataConcurrency+"=3")
	env, err = DefaultEnv(t.TempDir())
	if err != nil || env.MetadataConcurrency != MetadataReadConcurrencyLimit {
		t.Fatalf("raised metadata override=%d err=%v", env.MetadataConcurrency, err)
	}
}

func TestFetchRejectsDifferentSignedMetadataGenerations(t *testing.T) {
	f := newFixture(t, "v0.10.0")
	f.publish("v0.11.0", commitOf('b'), []byte("artifact"), true, nil)
	f.mu.Lock()
	older := append([]byte(nil), f.files["/latest/manifest.json"]...)
	f.mu.Unlock()
	f.publish("v0.12.0", commitOf('c'), []byte("artifact"), true, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "manifest.json") {
			w.Write(older)
			return
		}
		f.srv.Config.Handler.ServeHTTP(w, r)
	}))
	defer srv.Close()
	_, err := f.env.Fetch(context.Background(), srv.URL+"/latest")
	var re *release.Error
	if !errors.As(err, &re) || re.Code != release.CodeSignatureInvalid {
		t.Fatalf("mismatched signed generations accepted: %v", err)
	}
}
