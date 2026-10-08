package main

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/peerstore"
	contract "github.com/sainteye/clawdline/internal/domain/agenthandoff"
	domaincloud "github.com/sainteye/clawdline/internal/domain/cloud"
)

type peerCLI struct {
	identity adaptercloud.Identity
	key      domaincloud.DeviceKey
	api      *adaptercloud.AccountClient
	store    *peerstore.Store
}

func openPeerCLI() (*peerCLI, error) {
	parts, err := openCloud()
	if err != nil {
		return nil, err
	}
	identity, found, err := parts.identity.Load()
	if err != nil || !found || !identity.Valid() {
		return nil, errors.New("this machine must finish Cloud login first")
	}
	if err := adaptercloud.CheckEnvironment(identity, parts.settings); err != nil {
		return nil, err
	}
	key, found, err := parts.keys.DeviceKey()
	if err != nil || !found || !key.Valid() {
		return nil, errors.New("this machine has no readable Cloud signing key")
	}
	store, err := peerstore.Open(parts.keys.Dir())
	if err != nil {
		return nil, err
	}
	return &peerCLI{identity: identity, key: key,
		api: adaptercloud.NewAccountClient(parts.settings.APIBase), store: store}, nil
}

func cloudPeerCommand(args []string) {
	if len(args) == 0 {
		peerUsage()
		os.Exit(2)
	}
	peer, err := openPeerCLI()
	if err != nil {
		fmt.Fprintf(os.Stderr, cliCopy("cloud_peer", "error", "peer error: %v\n"), err)
		os.Exit(1)
	}
	defer peer.store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	switch args[0] {
	case "fingerprint":
		if len(args) == 1 {
			fmt.Printf(cliCopy("cloud_peer", "fingerprint", "machine %s signing fingerprint %s\n"),
				peer.identity.MachineID, peer.key.Fingerprint())
		} else {
			peerUsage()
			os.Exit(2)
		}
	case "start":
		if len(args) == 3 {
			err = peer.start(ctx, args[1], args[2])
		} else {
			peerUsage()
			os.Exit(2)
		}
	case "accept":
		if len(args) == 3 {
			err = peer.accept(ctx, args[1], args[2])
		} else {
			peerUsage()
			os.Exit(2)
		}
	case "sync":
		if len(args) == 3 {
			err = peer.sync(ctx, args[1], args[2])
		} else {
			peerUsage()
			os.Exit(2)
		}
	case "grant":
		if len(args) == 7 {
			err = peer.grant(ctx, args[1:])
		} else {
			peerUsage()
			os.Exit(2)
		}
	case "grant-sync":
		if len(args) == 2 {
			err = peer.grantSync(ctx, args[1])
		} else {
			peerUsage()
			os.Exit(2)
		}
	case "revoke-pair", "revoke-grant":
		if len(args) == 2 {
			err = peer.revoke(ctx, args[0], args[1])
		} else {
			peerUsage()
			os.Exit(2)
		}
	case "send":
		if len(args) == 9 {
			err = peer.send(ctx, args[1:])
		} else {
			peerUsage()
			os.Exit(2)
		}
	case "inbox":
		if len(args) == 3 || len(args) == 4 {
			before := ""
			if len(args) == 4 {
				before = args[3]
			}
			err = peer.inbox(ctx, args[1], args[2], before)
		} else {
			peerUsage()
			os.Exit(2)
		}
	case "outbox":
		if len(args) == 2 {
			err = peer.outbox(ctx, args[1])
		} else {
			peerUsage()
			os.Exit(2)
		}
	default:
		peerUsage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, cliCopy("cloud_peer", "error", "peer error: %v\n"), err)
		os.Exit(1)
	}
}

func peerUsage() {
	fmt.Fprintln(os.Stderr, cliCopy("cloud_peer", "usage.fingerprint", "usage: clawdline cloud peer fingerprint"))
	fmt.Fprintln(os.Stderr, cliCopy("cloud_peer", "usage.start", "usage: clawdline cloud peer start <target-machine-id> <target-fingerprint>"))
	fmt.Fprintln(os.Stderr, cliCopy("cloud_peer", "usage.accept", "       clawdline cloud peer accept <pair-id> <source-fingerprint>"))
	fmt.Fprintln(os.Stderr, cliCopy("cloud_peer", "usage.sync", "       clawdline cloud peer sync <pair-id> <target-fingerprint>"))
	fmt.Fprintln(os.Stderr, cliCopy("cloud_peer", "usage.grant", "       clawdline cloud peer grant <pair-id> <source-session> <source-generation> <target-session> <target-generation> <message|handoff|both>"))
	fmt.Fprintln(os.Stderr, cliCopy("cloud_peer", "usage.grant_sync", "       clawdline cloud peer grant-sync <grant-id>"))
	fmt.Fprintln(os.Stderr, cliCopy("cloud_peer", "usage.revoke", "       clawdline cloud peer revoke-pair|revoke-grant <id>"))
	fmt.Fprintln(os.Stderr, cliCopy("cloud_peer", "usage.send", "       clawdline cloud peer send <grant-id> <source-session> <source-generation> <target-machine> <target-session> <target-generation> <message|handoff> <text>"))
	fmt.Fprintln(os.Stderr, cliCopy("cloud_peer", "usage.inbox", "       clawdline cloud peer inbox <target-session> <target-generation> [before-request-id]"))
	fmt.Fprintln(os.Stderr, cliCopy("cloud_peer", "usage.outbox", "       clawdline cloud peer outbox <request-id>"))
}

func (p *peerCLI) send(ctx context.Context, args []string) error {
	grant, err := p.store.Grant(ctx, args[0])
	if err != nil {
		return err
	}
	if grant.Source.MachineID != p.identity.MachineID || grant.Source.SessionID != args[1] ||
		grant.Source.ExecutionGeneration != args[2] || grant.Target.MachineID != args[3] ||
		grant.Target.SessionID != args[4] || grant.Target.ExecutionGeneration != args[5] {
		return errors.New("the explicit source and destination do not match the pinned grant")
	}
	kind := contract.Kind(args[6])
	if kind != contract.Message && kind != contract.Handoff {
		return errors.New("kind must be message or handoff")
	}
	requestID := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, requestID); err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(args[7]))
	request := contract.Request{RequestID: hex.EncodeToString(requestID), Kind: kind,
		Source: grant.Source, Target: grant.Target, GrantID: grant.ID,
		BodyDigest: hex.EncodeToString(digest[:])}
	req, err := daemonRequest(http.MethodPost, "/v1/cloud/peer/send",
		map[string]any{"request": request, "body": args[7]})
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(refusalText(resp))
	}
	var answer struct {
		RequestID        string `json:"request_id"`
		Code             string `json:"code"`
		RelayAccepted    string `json:"relay_accepted"`
		MachineExecution string `json:"machine_execution"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&answer); err != nil {
		return err
	}
	if answer.Code != "" && answer.Code != "ok" {
		return fmt.Errorf("request %s: %s", request.RequestID, answer.Code)
	}
	fmt.Printf(cliCopy("cloud_peer", "sent", "request %s was written to the socket; check its relay receipt with cloud peer outbox; target execution remains unknown\n"), answer.RequestID)
	return nil
}

func (p *peerCLI) outbox(ctx context.Context, id string) error {
	req, err := daemonRequest(http.MethodGet, "/v1/cloud/peer/outbox/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(refusalText(resp))
	}
	var item peerstore.OutboxItem
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&item); err != nil {
		return err
	}
	if item.Request.RequestID != id {
		return errors.New("the peer relay receipt names another request")
	}
	fmt.Printf(cliCopy("cloud_peer", "outbox_receipt", "request %s: relay accepted %s; socket delivery %s; target execution unknown\n"),
		id, item.RelayAccepted, item.RelayDelivered)
	return nil
}

func (p *peerCLI) inbox(ctx context.Context, session, generation, before string) error {
	path := "/v1/cloud/peer/inbox?machine_id=" + url.QueryEscape(p.identity.MachineID) +
		"&session_id=" + url.QueryEscape(session) + "&execution_generation=" + url.QueryEscape(generation)
	if before != "" {
		path += "&before=" + url.QueryEscape(before)
	}
	req, err := daemonRequest(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(refusalText(resp))
	}
	var answer struct {
		MachineID          string `json:"machine_id"`
		SessionID          string `json:"session_id"`
		ExpectedGeneration string `json:"expected_generation"`
		NextBefore         string `json:"next_before"`
		Items              []struct {
			Request    contract.Request `json:"request"`
			BodyBase64 string           `json:"body_base64"`
			Receipt    struct {
				MachineExecution  string `json:"machine_execution"`
				SessionDelivered  string `json:"session_delivered"`
				AgentObserved     string `json:"agent_observed"`
				AgentAcknowledged string `json:"agent_acknowledged"`
			} `json:"receipt"`
		} `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&answer); err != nil {
		return err
	}
	if answer.MachineID != p.identity.MachineID || answer.SessionID != session || answer.ExpectedGeneration != generation {
		return errors.New("the peer inbox answer names another execution")
	}
	for _, item := range answer.Items {
		body, err := base64.StdEncoding.Strict().DecodeString(item.BodyBase64)
		if err != nil {
			return errors.New("the peer inbox body is invalid")
		}
		fmt.Printf("%q %q %q/%q -> %q/%q: %q\n", item.Request.RequestID, item.Request.Kind,
			item.Request.Source.MachineID, item.Request.Source.SessionID,
			item.Request.Target.MachineID, item.Request.Target.SessionID, string(body))
		fmt.Printf(cliCopy("cloud_peer", "inbox_receipt", "machine %s; Session %s; Agent observed %s; acknowledged %s\n"),
			item.Receipt.MachineExecution, item.Receipt.SessionDelivered,
			item.Receipt.AgentObserved, item.Receipt.AgentAcknowledged)
	}
	if answer.NextBefore != "" {
		fmt.Printf(cliCopy("cloud_peer", "inbox_next", "Next inbox page: clawdline cloud peer inbox %s %s %s\n"),
			session, generation, answer.NextBefore)
	}
	return nil
}

func (p *peerCLI) start(ctx context.Context, target, compared string) error {
	if target == p.identity.MachineID {
		return errors.New("source and target are the same machine")
	}
	machines, err := p.api.Machines(ctx, p.identity.MachineCredential)
	if err != nil {
		return err
	}
	matched := false
	for _, machine := range machines {
		if machine.ID == target && machine.RevokedAt == nil && machine.KeyFingerprint == compared {
			matched = true
		}
	}
	if !matched {
		return errors.New("the target machine or its compared fingerprint does not match the current account row")
	}
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	sourceEncryption := base64.StdEncoding.EncodeToString(private.PublicKey().Bytes())
	transcript := contract.PairTranscript{SourceMachineID: p.identity.MachineID,
		TargetMachineID: target, SourceFingerprint: p.key.Fingerprint(),
		TargetFingerprint: compared, SourceEncryptionKey: sourceEncryption}
	signature := base64.StdEncoding.EncodeToString(p.key.Sign(contract.OfferBytes(transcript)))
	started, err := p.api.StartPeerPair(ctx, p.identity.MachineCredential, adaptercloud.PeerPairStart{
		TargetMachineID: target, TargetKeyFingerprint: compared,
		SourceEncryptionKey: sourceEncryption, SourceOfferSignature: signature,
	})
	if err != nil {
		return err
	}
	expires, err := time.Parse(time.RFC3339Nano, started.ExpiresAt)
	if err != nil || started.PairID == "" || started.SourceMachineID != p.identity.MachineID ||
		started.TargetMachineID != target || started.TargetKeyFingerprint != compared {
		return errors.New("the peer API returned an inconsistent pair")
	}
	err = p.store.SavePending(ctx, peerstore.Pair{
		ID: started.PairID, AccountID: p.identity.AccountID,
		SourceMachineID: p.identity.MachineID, TargetMachineID: target,
		SourcePublicKey:   base64.StdEncoding.EncodeToString(p.key.PublicKey()),
		SourceFingerprint: p.key.Fingerprint(), TargetFingerprint: compared,
		SourceEncryptionKey: sourceEncryption, SourceSignature: signature,
		LocalPrivateKey: private.Bytes(), ExpiresAt: expires,
	})
	if err != nil {
		_ = p.api.RevokePeerPair(ctx, p.identity.MachineCredential, started.PairID)
		return err
	}
	fmt.Printf(cliCopy("cloud_peer", "started", "pair %s is waiting; compare source fingerprint %s on the target machine\n"), started.PairID, p.key.Fingerprint())
	return nil
}

func (p *peerCLI) accept(ctx context.Context, id, compared string) error {
	pair, err := p.api.GetPeerPair(ctx, p.identity.MachineCredential, id)
	if err != nil {
		return err
	}
	if pair.TargetMachineID != p.identity.MachineID || pair.SourceKeyFingerprint != compared ||
		pair.TargetKeyFingerprint != p.key.Fingerprint() || pair.AcceptedAt != nil ||
		pair.RevokedAt != nil {
		return errors.New("the pending pair does not match this machine and the compared source fingerprint")
	}
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	targetEncryption := base64.StdEncoding.EncodeToString(private.PublicKey().Bytes())
	transcript := pairTranscript(pair)
	transcript.TargetEncryptionKey = targetEncryption
	transcript.TargetAcceptSignature = base64.StdEncoding.EncodeToString(p.key.Sign(contract.AcceptBytes(transcript)))
	if err := contract.VerifyTranscript(transcript, p.identity.MachineID, compared); err != nil {
		return err
	}
	accepted, err := p.api.AcceptPeerPair(ctx, p.identity.MachineCredential, id, adaptercloud.PeerPairAccept{
		SourceKeyFingerprint: compared, TargetEncryptionKey: targetEncryption,
		TargetAcceptSignature: transcript.TargetAcceptSignature,
	})
	if err != nil {
		return err
	}
	if err := p.pinAccepted(ctx, accepted, private.Bytes(), compared); err != nil {
		_ = p.api.RevokePeerPair(ctx, p.identity.MachineCredential, id)
		return err
	}
	fmt.Printf(cliCopy("cloud_peer", "accepted", "pair %s is active here; ask the source machine to sync it\n"), id)
	return nil
}

func (p *peerCLI) sync(ctx context.Context, id, compared string) error {
	pending, err := p.store.Pair(ctx, id)
	if err != nil {
		return err
	}
	if pending.TargetFingerprint != compared || pending.SourceMachineID != p.identity.MachineID {
		return errors.New("the compared target fingerprint does not match the pending pair")
	}
	accepted, err := p.api.GetPeerPair(ctx, p.identity.MachineCredential, id)
	if err != nil {
		return err
	}
	if err := p.pinAccepted(ctx, accepted, pending.LocalPrivateKey, compared); err != nil {
		return err
	}
	fmt.Printf(cliCopy("cloud_peer", "synced", "pair %s is pinned on this machine\n"), id)
	return nil
}

func pairTranscript(pair adaptercloud.PeerPair) contract.PairTranscript {
	t := contract.PairTranscript{
		PairID: pair.PairID, SourceMachineID: pair.SourceMachineID, TargetMachineID: pair.TargetMachineID,
		SourcePublicKey: pair.SourcePublicKey, TargetPublicKey: pair.TargetPublicKey,
		SourceFingerprint: pair.SourceKeyFingerprint, TargetFingerprint: pair.TargetKeyFingerprint,
		SourceEncryptionKey: pair.SourceEncryptionKey, SourceOfferSignature: pair.SourceOfferSignature,
	}
	if pair.TargetEncryptionKey != nil {
		t.TargetEncryptionKey = *pair.TargetEncryptionKey
	}
	if pair.TargetAcceptSignature != nil {
		t.TargetAcceptSignature = *pair.TargetAcceptSignature
	}
	return t
}

func (p *peerCLI) pinAccepted(ctx context.Context, pair adaptercloud.PeerPair, private []byte, compared string) error {
	if pair.PairID == "" || pair.AcceptedAt == nil || pair.RevokedAt != nil ||
		pair.TargetEncryptionKey == nil || pair.TargetAcceptSignature == nil {
		return errors.New("the peer API has no active signed pair")
	}
	expires, err := time.Parse(time.RFC3339Nano, pair.ExpiresAt)
	if err != nil {
		return err
	}
	return p.store.PinPair(ctx, peerstore.Pair{
		ID: pair.PairID, AccountID: p.identity.AccountID,
		SourceMachineID: pair.SourceMachineID, TargetMachineID: pair.TargetMachineID,
		SourcePublicKey: pair.SourcePublicKey, TargetPublicKey: pair.TargetPublicKey,
		SourceFingerprint: pair.SourceKeyFingerprint, TargetFingerprint: pair.TargetKeyFingerprint,
		SourceEncryptionKey: pair.SourceEncryptionKey, TargetEncryptionKey: *pair.TargetEncryptionKey,
		SourceSignature: pair.SourceOfferSignature, TargetSignature: *pair.TargetAcceptSignature,
		LocalPrivateKey: private, ExpiresAt: expires,
	}, p.identity.MachineID, compared)
}

func (p *peerCLI) grant(ctx context.Context, args []string) error {
	pair, err := p.store.Pair(ctx, args[0])
	if err != nil {
		return err
	}
	if pair.TargetMachineID != p.identity.MachineID || pair.TargetSignature == "" ||
		!pair.RevokedAt.IsZero() || !time.Now().Before(pair.ExpiresAt) {
		return errors.New("this machine has no active recipient pair")
	}
	scopes := []string{args[5]}
	if args[5] == "both" {
		scopes = []string{"message", "handoff"}
	} else if args[5] != "message" && args[5] != "handoff" {
		return errors.New("scope must be message, handoff or both")
	}
	created, err := p.api.CreatePeerGrant(ctx, p.identity.MachineCredential, adaptercloud.PeerGrantCreate{
		PairID: pair.ID, SourceSessionID: args[1], SourceExecutionGeneration: args[2],
		TargetSessionID: args[3], TargetExecutionGeneration: args[4],
		Scopes: scopes, ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	})
	if err != nil {
		return err
	}
	if err := p.pinGrant(ctx, created); err != nil {
		_ = p.api.RevokePeerGrant(ctx, p.identity.MachineCredential, created.GrantID)
		return err
	}
	fmt.Printf(cliCopy("cloud_peer", "granted", "grant %s permits %s; source machine must sync it\n"), created.GrantID, strings.Join(scopes, ", "))
	return nil
}

func (p *peerCLI) grantSync(ctx context.Context, id string) error {
	grant, err := p.api.GetPeerGrant(ctx, p.identity.MachineCredential, id)
	if err != nil {
		return err
	}
	if err := p.pinGrant(ctx, grant); err != nil {
		return err
	}
	fmt.Printf(cliCopy("cloud_peer", "grant_synced", "grant %s is pinned on this machine\n"), id)
	return nil
}

func (p *peerCLI) pinGrant(ctx context.Context, grant adaptercloud.PeerGrant) error {
	if grant.RevokedAt != nil || grant.GrantID == "" {
		return errors.New("the peer grant is revoked or missing")
	}
	expires, err := time.Parse(time.RFC3339Nano, grant.ExpiresAt)
	if err != nil {
		return err
	}
	row := peerstore.Grant{ID: grant.GrantID, PairID: grant.PairID,
		Source: grant.Source, Target: grant.Target,
		SourceKeyFingerprint: grant.SourceKeyFingerprint, ExpiresAt: expires}
	for _, scope := range grant.Scopes {
		switch scope {
		case "message":
			row.AllowMessage = true
		case "handoff":
			row.AllowHandoff = true
		default:
			return errors.New("the peer API returned an unknown scope")
		}
	}
	return p.store.PinGrant(ctx, row)
}

func (p *peerCLI) revoke(ctx context.Context, kind, id string) error {
	var localErr, apiErr error
	if kind == "revoke-pair" {
		localErr = p.store.RevokePair(ctx, id, time.Now())
		apiErr = p.api.RevokePeerPair(ctx, p.identity.MachineCredential, id)
	} else {
		localErr = p.store.RevokeGrant(ctx, id, time.Now())
		apiErr = p.api.RevokePeerGrant(ctx, p.identity.MachineCredential, id)
	}
	if localErr != nil || apiErr != nil {
		return fmt.Errorf("local revoke: %v; Cloud revoke: %v", localErr, apiErr)
	}
	fmt.Printf(cliCopy("cloud_peer", "revoked", "%s %s is revoked locally and in Cloud\n"), kind, id)
	return nil
}
