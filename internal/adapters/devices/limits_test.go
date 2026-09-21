package devices

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/domain/auth"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

func devicesOf(n int) auth.State {
	var s auth.State
	for i := 0; i < n; i++ {
		s.Devices = append(s.Devices, auth.Device{
			ID: fmt.Sprintf("d%d", i), Name: "n", Hash: digest(strconv.Itoa(i)),
			Caps: auth.NewCaps(auth.Read), Created: time.Unix(int64(1+i), 0), Approved: true,
		})
	}
	return s
}

// limits N13: every sign-in added a device and nothing ever stopped it. At the
// register's limit an addition is refused, typed, with nothing written; a
// revoke and a change to a device already there still save.
func TestTheDeviceListRefusesAnAdditionAtItsLimit(t *testing.T) {
	dir := t.TempDir()
	f, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Load(); err != nil {
		t.Fatal(err)
	}
	f.SetDeviceLimit(3)
	for n := 1; n <= 3; n++ {
		if err := f.Save(devicesOf(n)); err != nil {
			t.Fatalf("%d devices: %v", n, err)
		}
	}
	err = f.Save(devicesOf(4))
	if !errors.Is(err, ErrDeviceListFull) {
		t.Fatalf("the fourth device: %v, want ErrDeviceListFull", err)
	}
	if got, _ := f.Load(); len(got.Devices) != 3 {
		t.Fatalf("a refused save wrote %d devices", len(got.Devices))
	}
	r := f.DevicesReading()
	if !r.Known || r.Used != 3 || r.Counters.Refused != 1 || r.Counters.LastActionAt.IsZero() {
		t.Fatalf("reading: %+v", r)
	}

	// An override lowered under what the file holds: nothing may be added,
	// and the list may still be changed and shortened.
	f.SetDeviceLimit(1)
	changed := devicesOf(3)
	changed.Devices[0].Caps = auth.NewCaps(auth.Read, auth.Send)
	if err := f.Save(changed); err != nil {
		t.Fatalf("changing a device already there: %v", err)
	}
	if err := f.Save(devicesOf(2)); err != nil {
		t.Fatalf("a revoke: %v", err)
	}
	if err := f.Save(devicesOf(3)); !errors.Is(err, ErrDeviceListFull) {
		t.Fatalf("adding back over the limit: %v", err)
	}
}

// The reason the row exists: the device list must speak before the read bound
// does, because past the read bound the daemon does not start (limits N13).
// The register's limit, every row written at its longest and escaped at its
// worst, is still far under what Load reads — and it loads.
func TestTheDeviceLimitSpeaksBeforeTheReadBound(t *testing.T) {
	limit := int(capacity.Default(capacity.DevicesList))
	worst := auth.State{}
	for i := 0; i < limit; i++ {
		// The id has no spaces or controls (validText), but '<' is escaped to
		// six bytes; the name may hold anything, and a control byte is six too.
		id := fmt.Sprintf("%04d", i) + strings.Repeat("<", idLimit-4)
		worst.Devices = append(worst.Devices, auth.Device{
			ID: id, Name: strings.Repeat("\x01", storedNameLimit), Hash: digest(id),
			// The longest times the file writes: nanoseconds still in range.
			Caps: auth.NewCaps(auth.Read, auth.Send, auth.Admin), Created: time.Unix(9_000_000_000, 123_456_789),
			LastSeen: time.Unix(9_000_000_000, 123_456_789), Approved: true,
		})
	}
	dir := t.TempDir()
	f, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Save(worst); err != nil {
		t.Fatalf("the register's limit at its worst could not be saved: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, StoreFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size()*2 > storeLimit {
		t.Fatalf("%d devices at their longest are %d bytes, not under half of the read bound %d", limit, info.Size(), storeLimit)
	}
	if got, err := f.Load(); err != nil || len(got.Devices) != limit {
		t.Fatalf("the file the limit allows does not load: %d devices, %v", len(got.Devices), err)
	}
	t.Logf("%d devices at their longest: %d bytes of the %d the daemon reads", limit, info.Size(), storeLimit)
}

// Under the row limit, the floor: a file Load would refuse is never written,
// whatever the count.
func TestAFileTooLargeToLoadIsNeverWritten(t *testing.T) {
	dir := t.TempDir()
	f, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Save(devicesOf(1)); err != nil {
		t.Fatal(err)
	}
	f.SetDeviceLimit(1 << 30)
	huge := devicesOf(1)
	for i := 0; len(huge.Devices) < 20_000; i++ {
		huge.Devices = append(huge.Devices, auth.Device{
			ID: fmt.Sprintf("x%d", i), Name: strings.Repeat("\x01", storedNameLimit), Hash: digest(fmt.Sprint(i)),
			Caps: auth.NewCaps(auth.Read), Approved: true,
		})
	}
	if err := f.Save(huge); !errors.Is(err, ErrDeviceListTooLarge) {
		t.Fatalf("a file past the read bound: %v", err)
	}
	if got, err := f.Load(); err != nil || len(got.Devices) != 1 {
		t.Fatalf("after the refusal the file holds %d devices, %v", len(got.Devices), err)
	}
}

// Before the file has been read, how many devices it holds is not known, and
// the reading says so rather than zero (DG-7).
func TestAnUnreadDeviceListIsUnknown(t *testing.T) {
	f, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if r := f.DevicesReading(); r.Known || r.Err == "" {
		t.Fatalf("reading before any load: %+v", r)
	}
	if _, err := f.Load(); err != nil {
		t.Fatal(err)
	}
	if r := f.DevicesReading(); !r.Known || r.Used != 0 {
		t.Fatalf("reading of a machine with no device file: %+v", r)
	}
}

// The audit is two records, and which one an event goes to is decided by name.
// Every event name this repository hands to an audit function is read out of
// the source here and must be on exactly one of the two lists: a new event
// nobody classified is a failing test, not a guess.
func TestEveryAuditedEventIsClassifiedOnce(t *testing.T) {
	root := moduleRoot(t)
	names := auditedEvents(t, root)
	if len(names) < 20 {
		t.Fatalf("found %d audited event names; the scan is not reading the tree", len(names))
	}
	for name, at := range names {
		sec, jour := SecurityEvent(name), journalEvents[name]
		if sec == jour {
			t.Errorf("%s (%s) is security=%v journal=%v: it must be on exactly one list", name, at, sec, jour)
		}
	}
	for _, want := range []string{"pair.done", "device.revoke", "password.fail", "push.subscribe", "orchestrator.schedule.created"} {
		if !SecurityEvent(want) || JournalEvent(want) {
			t.Errorf("%s is not the security audit's", want)
		}
	}
	for _, want := range []string{"orchestrator.schedule.run", "orchestrator.schedule.skipped"} {
		if !JournalEvent(want) {
			t.Errorf("%s is not the journal's", want)
		}
	}
	if JournalEvent("something.nobody.named") {
		t.Error("an unclassified event was taken for the journal's")
	}
}

// auditedEvents is every string literal passed as the first argument to a
// function or method named audit, Audit or auditPush under internal/ and cmd/.
func auditedEvents(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" || d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				var name string
				switch fn := call.Fun.(type) {
				case *ast.Ident:
					name = fn.Name
				case *ast.SelectorExpr:
					name = fn.Sel.Name
				}
				if name != "audit" && name != "Audit" && name != "auditPush" {
					return true
				}
				if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if v, err := strconv.Unquote(lit.Value); err == nil {
						out[v] = fset.Position(lit.Pos()).String()
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's directory")
		}
		dir = parent
	}
}
