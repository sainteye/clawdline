package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/install"
	"github.com/sainteye/clawdline/internal/contract"
)

// Explicit opt-in: this test registers only its unique, temporary LaunchAgent.
// It never uses the installed service, port, state directory or tmux server.
func TestLaunchdUpdateRecovery(t *testing.T) {
	if os.Getenv("CLAWDLINE_TEST_LAUNCHD_RECOVERY") != "1" {
		t.Skip("set CLAWDLINE_TEST_LAUNCHD_RECOVERY=1 in a macOS GUI login")
	}
	for _, named := range []bool{false, true} {
		t.Run(fmt.Sprintf("named_previous_%v", named), func(t *testing.T) {
			f := newFixture(t, "v0.10.0")
			f.env.GOOS = "darwin"
			f.env.Health = nil
			f.env.HealthWait = 10 * time.Second
			f.env.Poll = 50 * time.Millisecond
			l := f.env.Layout
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			contents, err := os.ReadFile(binary)
			if err != nil {
				t.Fatal(err)
			}
			for _, version := range []string{"v0.10.0", "v0.11.0"} {
				stamp := commitOf('a')
				if version == "v0.11.0" {
					stamp = commitOf('b')
				}
				f.installRelease(version, stamp)
				build, _ := json.Marshal(map[string]string{"stamp": stamp, "version": version})
				if err := os.WriteFile(filepath.Join(l.ReleaseDir(version), "dist", "BUILD.json"), build, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(l.ReleaseDir(version), "clawdline"), contents, 0700); err != nil {
					t.Fatal(err)
				}
				if named || version == "v0.11.0" {
					if err := os.WriteFile(filepath.Join(l.ReleaseDir(version), install.DaemonBinary), contents, 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := l.InstallServiceBinary("v0.10.0"); err != nil {
				t.Fatal(err)
			}
			// The initial job is deliberately the legacy CLI, as left by a pre-rename updater.
			if named {
				if err := os.Link(filepath.Join(l.ReleaseDir("v0.10.0"), "clawdline"), l.LegacyServiceBin()); err != nil {
					t.Fatal(err)
				}
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := listener.Addr().(*net.TCPAddr).Port
			listener.Close()
			label := fmt.Sprintf("com.clawdline.update-recovery-test.%d.%s.%v", os.Getpid(), filepath.Base(filepath.Dir(f.env.StateDir)), named)
			svc := install.ServiceFile{Supervisor: "launchd", Name: label, Domain: fmt.Sprintf("gui/%d", os.Getuid()), Port: port, Root: l.Root, File: filepath.Join(f.env.StateDir, "service.plist")}
			plist := install.LaunchdPlist(label, install.ServiceSpec{Exec: l.LegacyServiceBin(), Port: port, Home: f.env.StateDir, StateDir: f.env.StateDir, InstallRoot: l.Root, Autostart: true, LogPath: filepath.Join(f.env.StateDir, "launchd.log")})
			plist = strings.Replace(plist, "<key>EnvironmentVariables</key>\n\t<dict>", "<key>EnvironmentVariables</key>\n\t<dict>\n<key>CLAWDLINE_TEST_LAUNCHD_HELPER</key><string>1</string>", 1)
			plist = strings.Replace(plist, "<string>serve</string>", "<string>-test.run=^TestLaunchdRecoveryHelper$</string>", 1)
			if err := os.WriteFile(svc.File, []byte(plist), 0600); err != nil {
				t.Fatal(err)
			}
			if err := install.WriteServiceFile(f.env.StateDir, svc); err != nil {
				t.Fatal(err)
			}
			f.env.Run = ExecRunner
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), serviceCommandTimeout)
				defer cancel()
				if _, err := ExecRunner(ctx, "launchctl", "bootout", svc.Domain+"/"+svc.Name); err != nil {
					t.Errorf("fixture cleanup: %v", err)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), serviceCommandTimeout)
			defer cancel()
			if err := f.env.bootstrapService(ctx, svc); err != nil {
				t.Fatal(err)
			}
			if err := f.env.waitHealthy(context.Background(), svc, Served{Commit: commitOf('a')}); err != nil {
				t.Fatal(err)
			}
			injected := false
			f.env.Run = func(ctx context.Context, command string, args ...string) ([]byte, error) {
				if command == "launchctl" && args[0] == "bootstrap" && !injected {
					injected = true
					return nil, fmt.Errorf("injected first-bootstrap I/O error after real bootout")
				}
				return ExecRunner(ctx, command, args...)
			}
			p := Pending{From: "v0.10.0", To: "v0.11.0", FromCommit: commitOf('a'), ToCommit: commitOf('b'), Phase: phaseSwitch}
			if err := f.env.writePending(p); err != nil {
				t.Fatal(err)
			}
			if err := f.env.Finish(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !injected || f.apply().State != contract.UpdateApplyStateHealthy {
				t.Fatalf("update not accepted: injected=%v status=%+v", injected, f.apply())
			}
			t.Log("legacy job migrated after a failed bootstrap; HTTP console and candidate stamp verified")
			// Reproduce the actual outage: the job is gone, while the on-disk plist
			// still names the daemon used by the rollback release as well.
			if _, err := ExecRunner(context.Background(), "launchctl", "bootout", svc.Domain+"/"+svc.Name); err != nil {
				t.Fatal(err)
			}
			p.Phase = phaseRollback
			p.Reason = &contract.UpdateApplyError{Code: CodeSupervisorFailed, Detail: "injected service loss"}
			if err := f.env.writePending(p); err != nil {
				t.Fatal(err)
			}
			if err := f.env.Finish(context.Background()); err != nil {
				t.Fatal(err)
			}
			if f.apply().State != contract.UpdateApplyStateRolledBack {
				t.Fatalf("rollback not accepted: %+v", f.apply())
			}
			if err := f.env.httpHealth(context.Background(), port, "", Served{Commit: commitOf('a')}); err != nil {
				t.Fatal(err)
			}
			t.Log("unloaded job restored; HTTP console and previous stamp verified")
		})
	}
}

// The fixture answers with its startup stamp, so a still-running old process
// cannot satisfy the candidate health check merely because current changed.
func TestLaunchdRecoveryHelper(t *testing.T) {
	if os.Getenv("CLAWDLINE_TEST_LAUNCHD_HELPER") != "1" {
		t.Skip("launchd fixture helper only")
	}
	root := os.Getenv("CLAWDLINE_NEXT_INSTALL_ROOT")
	body, err := os.ReadFile(filepath.Join(root, "current", "dist", "BUILD.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stamp map[string]string
	if err := json.Unmarshal(body, &stamp); err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(os.Getenv("CLAWDLINE_NEXT_PORT"))
	if err != nil || port == 7727 {
		t.Fatal("fixture needs its own port")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/BUILD.json", func(w http.ResponseWriter, r *http.Request) { w.Write(body) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "fixture console") })
	if err := http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), mux); err != nil {
		t.Fatal(err)
	}
}
