package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/cloudkeys"
	domain "github.com/sainteye/clawdline/internal/domain/cloud"
)

func cloudViewerPairCommand(args []string) {
	fs := flag.NewFlagSet("cloud viewer pair", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	machineID := fs.String("machine", "", "machine ID")
	fingerprint := fs.String("fingerprint", "", "fingerprint shown by that machine")
	wait := fs.Duration("wait", 10*time.Minute, "how long to wait for the handover")
	if fs.Parse(args) != nil || fs.NArg() != 0 || *machineID == "" || *fingerprint == "" || *wait <= 0 {
		fmt.Fprintln(os.Stderr, cliCopy("viewer", "pair_usage", "usage: clawdline cloud viewer pair --machine ID --fingerprint FINGERPRINT [--wait 10m]"))
		os.Exit(2)
	}
	parts, err := openCloud()
	if err != nil {
		fail(err)
	}
	identity, found, err := cloud.NewViewerIdentityStore(parts.keys.Dir()).Load()
	if err != nil || !found || identity.APIBase != parts.settings.APIBase {
		fmt.Fprintln(os.Stderr, cliCopy("viewer", "login_required", "viewer_login_required: authorize this local viewer first"))
		os.Exit(1)
	}
	key, found, err := parts.keys.ViewerKey()
	if err != nil || !found {
		fail(errors.New("viewer_key_missing"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), *wait+time.Minute)
	defer cancel()
	if err := viewerPairRun(ctx, os.Stdout, cloud.NewAccountClient(parts.settings.APIBase), parts.keys, identity, key.PublicKey(), *machineID, *fingerprint, *wait); err != nil {
		fmt.Fprintf(os.Stderr, "clawdline cloud viewer: pair_failed: %v\n", err)
		os.Exit(1)
	}
}

func cloudViewerMachinesCommand(args []string) {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, cliCopy("viewer", "machines_usage", "usage: clawdline cloud viewer machines"))
		os.Exit(2)
	}
	parts, err := openCloud()
	if err != nil {
		fail(err)
	}
	identity, found, err := cloud.NewViewerIdentityStore(parts.keys.Dir()).Load()
	if err != nil || !found || identity.APIBase != parts.settings.APIBase {
		fmt.Fprintln(os.Stderr, cliCopy("viewer", "login_required", "viewer_login_required: authorize this local viewer first"))
		os.Exit(1)
	}
	if err := viewerMachineListRun(context.Background(), os.Stdout, cloud.NewAccountClient(parts.settings.APIBase), cloud.NewViewerPinStore(parts.keys.Dir()), identity); err != nil {
		fmt.Fprintf(os.Stderr, "clawdline cloud viewer: machine_list_failed: %v\n", err)
		os.Exit(1)
	}
}

func viewerMachineListRun(ctx context.Context, out io.Writer, client *cloud.AccountClient, pins *cloud.ViewerPinStore, identity cloud.ViewerIdentity) error {
	local, err := pins.Load(identity)
	if err != nil {
		return err
	}
	machines, err := client.Machines(ctx, identity.ViewerCredential)
	if err != nil {
		return err
	}
	if len(machines) == 0 {
		fmt.Fprintln(out, cliCopy("viewer", "machines_none", "no Cloud machines are registered on this account"))
		return nil
	}
	for _, machine := range machines {
		key, err := base64.StdEncoding.DecodeString(machine.PublicKey)
		if err != nil || len(key) != 32 || domain.Fingerprint(key) != machine.KeyFingerprint {
			return errors.New("Cloud machine roster has an invalid signing key")
		}
		state := cliCopy("viewer", "machines_unpaired", "unpaired")
		if machine.RevokedAt != nil {
			state = cliCopy("viewer", "machines_revoked", "revoked")
		}
		if pin, ok := local[machine.ID]; ok && machine.RevokedAt == nil && pin.MachineSigningKey == machine.PublicKey {
			state = cliCopy("viewer", "machines_paired", "paired")
		}
		fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", machine.ID, machine.KeyFingerprint, state, machine.Name)
	}
	return nil
}

func viewerPairRun(ctx context.Context, out io.Writer, client *cloud.AccountClient, keys *cloudkeys.Files, identity cloud.ViewerIdentity, signingKey []byte, machineID, expectedFingerprint string, wait time.Duration) error {
	machines, err := client.Machines(ctx, identity.ViewerCredential)
	if err != nil {
		return err
	}
	var machine *cloud.MachineRecord
	for i := range machines {
		if machines[i].ID == machineID && machines[i].RevokedAt == nil {
			machine = &machines[i]
			break
		}
	}
	if machine == nil {
		return errors.New("target machine is absent or revoked")
	}
	machineKey, err := base64.StdEncoding.DecodeString(machine.PublicKey)
	if err != nil || len(machineKey) != 32 || domain.Fingerprint(machineKey) != machine.KeyFingerprint {
		return errors.New("Cloud machine roster has an invalid signing key")
	}
	if machine.KeyFingerprint != expectedFingerprint {
		return errors.New("machine fingerprint does not match the one supplied")
	}
	privateKey, err := domain.NewX25519PrivateKey(nil)
	if err != nil {
		return err
	}
	publicKey, err := domain.X25519PublicKey(privateKey)
	if err != nil {
		return err
	}
	start, err := client.StartViewerPairing(ctx, identity.ViewerCredential, domain.Fingerprint(signingKey))
	if err != nil {
		return err
	}
	claimNonce, err := base64.StdEncoding.DecodeString(start.ClaimNonce)
	if err != nil || len(claimNonce) != domain.PairingNonceBytes {
		return errors.New("Cloud returned an invalid claim nonce")
	}
	pairingNonce := make([]byte, domain.PairingNonceBytes)
	if _, err := rand.Read(pairingNonce); err != nil {
		return err
	}
	for string(pairingNonce) == string(claimNonce) {
		if _, err := rand.Read(pairingNonce); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(wait)
	serverExpiry, err := time.Parse(time.RFC3339Nano, start.ExpiresAt)
	if err != nil {
		return err
	}
	if serverExpiry.Before(deadline) {
		deadline = serverExpiry
	}
	expiresAt := deadline.UnixMilli()
	if expiresAt > time.Now().Add(domain.PairingOfferLifetimeMS*time.Millisecond).UnixMilli() {
		expiresAt = time.Now().Add(domain.PairingOfferLifetimeMS * time.Millisecond).UnixMilli()
	}
	offer := domain.PairingOffer{
		PairingID: start.PairingID, ClaimNonce: start.ClaimNonce, PairingNonce: base64.StdEncoding.EncodeToString(pairingNonce),
		AccountID: identity.AccountID, ViewerDeviceID: identity.DeviceID,
		ViewerSigningKey: base64.StdEncoding.EncodeToString(signingKey), ViewerEphemeralKey: base64.StdEncoding.EncodeToString(publicKey),
		ViewerFingerprint: domain.Fingerprint(signingKey), ExpiresAt: expiresAt,
	}
	if err := offer.Validate(time.Now().UnixMilli()); err != nil {
		return err
	}
	fmt.Fprintf(out, cliCopy("viewer", "pair_offer", "Machine fingerprint %s\nRun on that machine: clawdline cloud pair --offer %s\nWaiting for encrypted handover…\n"), expectedFingerprint, offer.Fragment())
	for time.Now().Before(deadline) {
		claim, err := client.ClaimViewerPairing(ctx, identity.ViewerCredential, start.PairingID, start.ClaimNonce)
		if errors.Is(err, cloud.ErrPairingPending) {
			if err := viewerWait(ctx, 5*time.Second); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if claim.SenderDeviceID != machineID {
			return errors.New("handover sender is not the selected machine")
		}
		blob, err := base64.StdEncoding.DecodeString(claim.Ciphertext)
		if err != nil {
			return err
		}
		wrapper, err := domain.DecodePairingWrapper(blob)
		if err != nil {
			return err
		}
		handover, err := domain.OpenPairingHandover(wrapper, offer, privateKey, claim.SenderDeviceID, time.Now().UnixMilli())
		if err != nil {
			return err
		}
		if handover.AccountID != identity.AccountID || handover.MachineID != machineID || handover.MachineFingerprint != expectedFingerprint || handover.MachineSigningKey != machine.PublicKey {
			return errors.New("handover does not match the selected machine and registered key")
		}
		pin := cloud.ViewerMachinePin{MachineID: handover.MachineID, MachineSigningKey: handover.MachineSigningKey,
			MachineFingerprint: handover.MachineFingerprint, KeyID: handover.KeyID, MasterSecret: handover.MasterSecret}
		if err := cloud.NewViewerPinStore(keys.Dir()).Save(identity, pin); err != nil {
			return err
		}
		fmt.Fprintf(out, cliCopy("viewer", "pair_done", "Paired viewer with machine %s (%s).\n"), machineID, expectedFingerprint)
		return nil
	}
	return errors.New("pairing offer expired without a handover")
}

func viewerWait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
