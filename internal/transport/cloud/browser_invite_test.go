package cloud

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

// inviteCloud is Cloud and one browser together: it records what the machine
// sends and answers polls as the browser progresses.
type inviteCloud struct {
	mu        sync.Mutex
	id        string
	status    string
	browserY  []byte
	machineY  string
	deviceID  string
	publicKey string
	confirm   string
	handover  string
	admitEnds int64
	denied    bool
	revoked   []string
	admitErr  error
	// denyDelay stands for the network round trip a real deny takes.
	denyDelay time.Duration
	// reportKey, when set, is the key Cloud claims the browser has
	// (a substitution the machine must catch).
	reportKey string
}

func (c *inviteCloud) StartBrowserInvite(_ context.Context, _, _ string) (adaptercloud.BrowserInviteStart, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status = adaptercloud.InviteStatusWaiting
	return adaptercloud.BrowserInviteStart{InviteID: c.id, ExpiresAt: time.Now().Add(10 * time.Minute).UnixMilli()}, nil
}

func (c *inviteCloud) PollBrowserInvite(_ context.Context, _, id string) (adaptercloud.BrowserInvitePoll, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := adaptercloud.BrowserInvitePoll{InviteID: id, MachineID: "mac_test", Status: c.status,
		DeviceID: c.deviceID, DevicePublicKey: c.publicKey, BrowserConfirm: c.confirm}
	if c.reportKey != "" {
		p.DevicePublicKey = c.reportKey
	}
	if c.browserY != nil {
		p.BrowserMessage = base64.StdEncoding.EncodeToString(c.browserY)
	}
	return p, nil
}

func (c *inviteCloud) AnswerBrowserInvite(_ context.Context, _, _, m string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.machineY = m
	return nil
}

func (c *inviteCloud) AdmitBrowserInvite(_ context.Context, _, _, device, handover string, ends int64) (adaptercloud.BrowserInviteAdmit, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.admitErr != nil {
		return adaptercloud.BrowserInviteAdmit{}, c.admitErr
	}
	c.handover, c.admitEnds, c.status = handover, ends, adaptercloud.InviteStatusAdmitted
	return adaptercloud.BrowserInviteAdmit{DeviceID: device}, nil
}

func (c *inviteCloud) DenyBrowserInvite(context.Context, string, string) error {
	time.Sleep(c.denyDelay)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.denied, c.status = true, adaptercloud.InviteStatusDenied
	return nil
}

func (c *inviteCloud) RevokeInvitedBrowser(_ context.Context, _, device string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.revoked = append(c.revoked, device)
	return nil
}

// browser types code into the join page and runs the browser's half.
type inviteBrowser struct {
	party  *domaincloud.CPaceParty
	public string
	keys   domaincloud.InviteKeys
}

func (c *inviteCloud) join(t *testing.T, typed string) *inviteBrowser {
	t.Helper()
	id, secret, err := domaincloud.ParseInviteCode(typed)
	if err != nil {
		t.Fatal(err)
	}
	prs, ci, sid := domaincloud.InviteCPaceInputs(id, secret)
	party, err := domaincloud.NewCPaceParty(rand.Reader, prs, ci, sid, domaincloud.InviteBrowserAD, true)
	if err != nil {
		t.Fatal(err)
	}
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	b := &inviteBrowser{party: party, public: base64.StdEncoding.EncodeToString(pub)}
	c.mu.Lock()
	c.browserY, c.deviceID, c.publicKey, c.status = party.Message(), "dev_invited", b.public, adaptercloud.InviteStatusJoined
	c.mu.Unlock()
	return b
}

func (c *inviteCloud) confirmAs(t *testing.T, b *inviteBrowser, name string) {
	t.Helper()
	var machineY string
	inviteWaitUntil(t, func() bool { c.mu.Lock(); defer c.mu.Unlock(); machineY = c.machineY; return machineY != "" })
	raw, _ := base64.StdEncoding.DecodeString(machineY)
	isk, err := b.party.Finish(raw, domaincloud.InviteMachineAD)
	if err != nil {
		t.Fatal(err)
	}
	b.keys, _ = domaincloud.DeriveInviteKeys(isk, c.id)
	sealed, err := domaincloud.SealInviteConfirm(b.keys, domaincloud.InviteConfirm{InviteID: c.id, MachineID: "mac_test",
		DeviceID: "dev_invited", PublicKey: b.public, Name: name}, make([]byte, domaincloud.NonceBytes))
	if err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	c.confirm, c.status = sealed, adaptercloud.InviteStatusConfirmed
	c.mu.Unlock()
}

func inviteWaitUntil(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func testInvites(t *testing.T, c *inviteCloud) (*BrowserInvites, *adaptercloud.PinnedStore) {
	t.Helper()
	pinned := adaptercloud.NewPinnedStore(t.TempDir())
	device, err := domaincloud.NewDeviceKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	content, err := domaincloud.NewContentKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &BrowserInvites{AccountID: "usr_test", MachineID: "mac_test", Credential: "cred", Client: c,
		Keys:   func() (domaincloud.DeviceKey, domaincloud.ContentKey, error) { return device, content, nil },
		Pinned: pinned, PollInterval: time.Millisecond,
		Revoke: func(device string) (bool, error) {
			p := &Pairing{Pinned: pinned, OnInvitedRevoke: func(d string) error {
				return c.RevokeInvitedBrowser(context.Background(), "cred", d)
			}}
			return p.Revoke(device)
		}}, pinned
}

func invitePhaseOf(t *testing.T, b *BrowserInvites, id, want string) BrowserInviteView {
	t.Helper()
	var v BrowserInviteView
	inviteWaitUntil(t, func() bool { v, _ = b.Status(id); return v.Phase == want })
	return v
}

func TestAnInvitedBrowserIsPinnedWithItsExpiryThenHandedItsKeys(t *testing.T) {
	c := &inviteCloud{id: "K7M4Q"}
	invites, pinned := testInvites(t, c)
	started, err := invites.Start(context.Background(), "8h")
	if err != nil {
		t.Fatal(err)
	}
	if started.Phase != InvitePhaseWaiting || len(started.Code) != 12 || started.Code[:6] != "K7M4Q-" {
		t.Fatalf("start view: %+v", started)
	}
	browser := c.join(t, started.Code)
	c.confirmAs(t, browser, "Linux \x07Chrome")
	v := invitePhaseOf(t, invites, "K7M4Q", InvitePhaseConfirm)
	if v.ComparisonCode != browser.keys.Code || v.Code != "" || v.BrowserName != "Linux  Chrome" {
		t.Fatalf("confirm view: %+v, browser code %s", v, browser.keys.Code)
	}
	if rows, _ := pinned.Devices(); len(rows) != 0 {
		t.Fatalf("a browser was pinned before the person approved: %+v", rows)
	}
	if _, err := invites.Decide("K7M4Q", true); err != nil {
		t.Fatal(err)
	}
	v = invitePhaseOf(t, invites, "K7M4Q", InvitePhaseAdmitted)
	rows, _ := pinned.Devices()
	if len(rows) != 1 || !rows[0].Invited || rows[0].EffectiveScope() != adaptercloud.ScopeOperate ||
		rows[0].ExpiresAt == 0 || rows[0].ExpiresAt != v.AdmissionEndsAt || rows[0].PublicKey != browser.public {
		t.Fatalf("pin: %+v view %+v", rows, v)
	}
	if got := rows[0].ExpiresAt - time.Now().Unix(); got < 8*3600-5 || got > 8*3600 {
		t.Fatalf("an 8h invitation ends in %ds", got)
	}
	h, err := domaincloud.OpenInviteHandover(browser.keys, c.handover, "K7M4Q")
	if err != nil {
		t.Fatalf("the browser cannot open its handover: %v", err)
	}
	if h.MachineID != "mac_test" || h.DeviceID != "dev_invited" || h.ExpiresAt != c.admitEnds || h.MasterSecret == "" {
		t.Fatalf("handover: %+v", h)
	}
}

func TestAWrongSecretSpendsTheInvitationAndPinsNothing(t *testing.T) {
	c := &inviteCloud{id: "K7M4Q"}
	invites, pinned := testInvites(t, c)
	started, _ := invites.Start(context.Background(), "never")
	wrong := started.Code[:11] + string(map[bool]byte{true: 'A', false: 'B'}[started.Code[11] != 'A'])
	browser := c.join(t, wrong)
	c.confirmAs(t, browser, "Chrome")
	v := invitePhaseOf(t, invites, "K7M4Q", InvitePhaseFailed)
	if v.Error != "code_mismatch" || !c.denied {
		t.Fatalf("view %+v denied=%v", v, c.denied)
	}
	if rows, _ := pinned.Devices(); len(rows) != 0 {
		t.Fatalf("pinned after a wrong secret: %+v", rows)
	}
}

func TestACloudThatSubstitutesTheBrowsersKeyIsCaught(t *testing.T) {
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	c := &inviteCloud{id: "K7M4Q", reportKey: base64.StdEncoding.EncodeToString(other)}
	invites, pinned := testInvites(t, c)
	started, _ := invites.Start(context.Background(), "7d")
	browser := c.join(t, started.Code)
	c.confirmAs(t, browser, "Chrome")
	if v := invitePhaseOf(t, invites, "K7M4Q", InvitePhaseFailed); v.Error != "code_mismatch" {
		t.Fatalf("view %+v", v)
	}
	if rows, _ := pinned.Devices(); len(rows) != 0 {
		t.Fatalf("pinned a substituted key: %+v", rows)
	}
}

func TestDenyingAndAFailedAdmissionLeaveNothingAdmitted(t *testing.T) {
	c := &inviteCloud{id: "K7M4Q"}
	invites, pinned := testInvites(t, c)
	started, _ := invites.Start(context.Background(), "8h")
	c.confirmAs(t, c.join(t, started.Code), "Chrome")
	invitePhaseOf(t, invites, "K7M4Q", InvitePhaseConfirm)
	if _, err := invites.Decide("K7M4Q", false); err != nil {
		t.Fatal(err)
	}
	invitePhaseOf(t, invites, "K7M4Q", InvitePhaseDenied)
	if rows, _ := pinned.Devices(); len(rows) != 0 || !c.denied {
		t.Fatalf("after deny: %+v denied=%v", rows, c.denied)
	}

	if got := inviteErrorCode(fmt.Errorf("%w: /v1/pairing/browser-invites/admit answered 409: {\"error\":{\"code\":\"device_limit_reached\"}}", adaptercloud.ErrInviteUnusable)); got != "device_limit" {
		t.Fatalf("a full account reads as %q", got)
	}
	c2 := &inviteCloud{id: "M9Q2X", admitErr: errors.New("Cloud is down")}
	invites2, pinned2 := testInvites(t, c2)
	started, _ = invites2.Start(context.Background(), "8h")
	c2.confirmAs(t, c2.join(t, started.Code), "Chrome")
	invitePhaseOf(t, invites2, "M9Q2X", InvitePhaseConfirm)
	invites2.Decide("M9Q2X", true)
	invitePhaseOf(t, invites2, "M9Q2X", InvitePhaseFailed)
	rows, _ := pinned2.Devices()
	if len(rows) != 1 || rows[0].Active() {
		t.Fatalf("a failed admission left an active pin: %+v", rows)
	}
}

func TestInvitationsAreBoundedAndExpiryIsChecked(t *testing.T) {
	c := &inviteCloud{id: "K7M4Q"}
	invites, _ := testInvites(t, c)
	if _, err := invites.Start(context.Background(), "30d"); !errors.Is(err, ErrInviteExpiry) {
		t.Fatalf("an unknown expiry: %v", err)
	}
	for i, id := range []string{"AAAAA", "BBBBB", "CCCCC"} {
		c.id = id
		if _, err := invites.Start(context.Background(), "8h"); err != nil {
			t.Fatalf("invite %d: %v", i, err)
		}
	}
	c.id = "DDDDD"
	if _, err := invites.Start(context.Background(), "8h"); !errors.Is(err, ErrInviteNoRoom) {
		t.Fatalf("a fourth live invitation: %v", err)
	}
	if v, err := invites.Cancel(context.Background(), "AAAAA"); err != nil || v.Phase != InvitePhaseCancelled || v.Code != "" {
		t.Fatalf("cancel: %+v %v", v, err)
	}
	// The run stopping must not overwrite the person's cancel. A real deny is a network round
	// trip, so the run usually notices the cancel first; the fake waits to reproduce that.
	c.denyDelay = 20 * time.Millisecond
	for i := 0; i < 5; i++ {
		c.id = fmt.Sprintf("C%04d", i)
		if _, err := invites.Start(context.Background(), "8h"); err != nil {
			t.Fatal(err)
		}
		if _, err := invites.Cancel(context.Background(), c.id); err != nil {
			t.Fatal(err)
		}
		if v, _ := invites.Status(c.id); v.Phase != InvitePhaseCancelled {
			t.Fatalf("round %d: a cancelled invitation reads %q", i, v.Phase)
		}
	}
	if _, err := invites.Start(context.Background(), "8h"); err != nil {
		t.Fatalf("after a cancel: %v", err)
	}
}

func TestRevokingAnInvitedBrowserRefusesItHereAndTellsCloud(t *testing.T) {
	c := &inviteCloud{id: "K7M4Q"}
	invites, pinned := testInvites(t, c)
	started, _ := invites.Start(context.Background(), "never")
	c.confirmAs(t, c.join(t, started.Code), "Chrome")
	invitePhaseOf(t, invites, "K7M4Q", InvitePhaseConfirm)
	invites.Decide("K7M4Q", true)
	invitePhaseOf(t, invites, "K7M4Q", InvitePhaseAdmitted)
	changed, err := invites.RevokeInvited("dev_invited")
	if err != nil || !changed {
		t.Fatalf("revoke: %v %v", changed, err)
	}
	if refused, _ := pinned.Refused("dev_invited"); !refused || len(c.revoked) != 1 {
		t.Fatalf("refused=%v cloud=%v", refused, c.revoked)
	}
	listed, err := invites.InvitedBrowsers()
	if err != nil || len(listed) != 1 || listed[0].DeviceID != "dev_invited" || listed[0].Active || listed[0].RevokedAt == 0 {
		t.Fatalf("invited browsers after revoke: %+v %v", listed, err)
	}
	// An account device is not the invitation page's to throw out.
	plain := freshInvitePin(t, "dev_account")
	if err := pinned.Pin("usr_test", plain); err != nil {
		t.Fatal(err)
	}
	if _, err := invites.RevokeInvited("dev_account"); !errors.Is(err, ErrInviteUnknown) {
		t.Fatalf("revoking an account device through invitations: %v", err)
	}
}

func freshInvitePin(t *testing.T, id string) adaptercloud.PinnedDevice {
	t.Helper()
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	return adaptercloud.PinnedDevice{DeviceID: id, PublicKey: base64.StdEncoding.EncodeToString(pub),
		Fingerprint: "AAAA-BBBB-CCCC-DDDD", PairedAt: time.Now().Unix()}
}
