package codegraph

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type serviceStatus struct {
	State        string `json:"state"`
	Reason       string `json:"reason"`
	Generation   string `json:"generation"`
	Watching     bool   `json:"watching"`
	LastSyncedAt int64  `json:"last_synced_at"`
}
type QueryResult struct {
	Text       string `json:"text"`
	Generation string `json:"generation"`
}
type serviceFrame struct {
	ID     uint64          `json:"id"`
	Event  string          `json:"event"`
	Error  string          `json:"error"`
	Result json.RawMessage `json:"result"`
}
type service struct {
	root        string
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	input       *os.File
	pending     chan serviceFrame
	pendingID   uint64
	next        uint64
	gate        chan struct{}
	initialized chan struct{}
	ready       chan struct{}
	done        chan struct{}
	readyOnce   sync.Once
	lastUsed    atomic.Int64
	startErr    error
}

func (b *Builder) provisionService(root string) (*service, error) {
	if !b.enabled() {
		return nil, BuildError("disabled")
	}
	resolved, resolveErr := canonical(root)
	if resolveErr != nil {
		return nil, resolveErr
	}
	root = resolved
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, BuildError("unavailable")
	}
	s := b.services[root]
	created := false
	if s == nil {
		if len(b.services) >= ServiceRootsLimit {
			b.mu.Unlock()
			return nil, BuildError("service_full")
		}
		child, cancel := context.WithCancel(b.ctx)
		s = &service{root: root, ctx: child, cancel: cancel, gate: make(chan struct{}, 1), initialized: make(chan struct{}), ready: make(chan struct{}), done: make(chan struct{})}
		b.services[root] = s
		created = true
		b.servicesWG.Add(1)
		go func() { <-s.done; b.servicesWG.Done() }()
	}
	s.lastUsed.Store(time.Now().Unix())
	b.mu.Unlock()
	if created {
		go func() {
			startup, cancel := context.WithTimeout(s.ctx, time.Duration(BuildSecondsLimit)*time.Second)
			defer cancel()
			err := s.start(startup, b)
			s.mu.Lock()
			s.startErr = err
			s.mu.Unlock()
			close(s.initialized)
			if err != nil {
				s.stop()
				b.mu.Lock()
				if b.services[root] == s {
					delete(b.services, root)
				}
				b.mu.Unlock()
			}
		}()
	}
	return s, nil
}
func (b *Builder) ensureService(ctx context.Context, root string) (*service, error) {
	s, err := b.provisionService(root)
	if err != nil {
		return nil, err
	}
	select {
	case <-s.initialized:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	s.mu.Lock()
	err = s.startErr
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if s.exited() {
		return nil, BuildError("unavailable")
	}
	if !b.enabled() {
		s.stop()
		return nil, BuildError("disabled")
	}
	return s, nil
}
func (s *service) start(ctx context.Context, b *Builder) error {
	rt, err := b.runtime(ctx, false)
	if err != nil {
		close(s.done)
		return err
	}
	digest := sha256.Sum256([]byte(s.root))
	home := filepath.Join(b.cache, "homes", hex.EncodeToString(digest[:]))
	if err = os.MkdirAll(home, 0700); err != nil {
		close(s.done)
		return err
	}
	command := exec.CommandContext(s.ctx, rt.Node, "--liftoff-only", rt.Bridge, rt.Bundle, "serve", s.root, bridgeLimits())
	command.Dir = home
	command.Env = parserEnvironment(home)
	command.WaitDelay = time.Second
	inputRead, inputWrite, err := os.Pipe()
	if err != nil {
		close(s.done)
		return err
	}
	outputRead, outputWrite, err := os.Pipe()
	if err != nil {
		inputRead.Close()
		inputWrite.Close()
		close(s.done)
		return err
	}
	s.mu.Lock()
	s.input = inputWrite
	s.mu.Unlock()
	command.Stdin = inputRead
	command.Stdout = outputWrite
	var logs cappedOutput
	command.Stderr = &logs
	if err = command.Start(); err != nil {
		inputRead.Close()
		inputWrite.Close()
		outputRead.Close()
		outputWrite.Close()
		close(s.done)
		return BuildError("runtime_unverified")
	}
	inputRead.Close()
	outputWrite.Close()
	go func() {
		defer outputRead.Close()
		scanner := bufio.NewScanner(outputRead)
		scanner.Buffer(make([]byte, BuildLogBytesLimit), ReplyBytesLimit)
		for scanner.Scan() {
			var frame serviceFrame
			if json.Unmarshal(scanner.Bytes(), &frame) != nil {
				continue
			}
			if frame.Event == "ready" {
				s.readyOnce.Do(func() { close(s.ready) })
				continue
			}
			if frame.Event == "failed" {
				s.mu.Lock()
				s.startErr = BuildError(frame.Error)
				s.mu.Unlock()
				s.cancel()
				continue
			}
			s.mu.Lock()
			pending := s.pending
			matches := s.pendingID == frame.ID && frame.ID != 0
			s.mu.Unlock()
			if matches && pending != nil {
				select {
				case pending <- frame:
				default:
				}
			}
		}
		if scanner.Err() != nil {
			s.cancel()
		}
	}()
	go func() {
		_ = command.Wait()
		if len(logs.data) > 0 {
			_ = os.WriteFile(filepath.Join(b.cache, "last-service.log"), logs.data, 0600)
		}
		inputWrite.Close()
		close(s.done)
	}()
	select {
	case <-s.ready:
		return nil
	case <-s.done:
		s.mu.Lock()
		err := s.startErr
		s.mu.Unlock()
		if err != nil {
			return err
		}
		return BuildError("parser_failed")
	case <-ctx.Done():
		s.stop()
		return ctx.Err()
	}
}
func (s *service) exited() bool {
	select {
	case <-s.done:
		return true
	default:
		return false
	}
}
func (s *service) stop() {
	s.cancel()
	s.mu.Lock()
	if s.input != nil {
		_ = s.input.Close()
	}
	s.mu.Unlock()
}
func (s *service) join(ctx context.Context) error {
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *service) call(ctx context.Context, method, query string, result any) error {
	select {
	case s.gate <- struct{}{}:
	default:
		return BuildError("busy")
	}
	defer func() { <-s.gate }()
	s.lastUsed.Store(time.Now().Unix())
	ctx, cancel := context.WithTimeout(ctx, time.Duration(QuerySecondsLimit)*time.Second)
	defer cancel()
	pending := make(chan serviceFrame, 1)
	s.mu.Lock()
	s.next++
	id := s.next
	s.pendingID = id
	s.pending = pending
	input := s.input
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.pending = nil; s.pendingID = 0; s.mu.Unlock() }()
	body, _ := json.Marshal(map[string]any{"id": id, "method": method, "query": query})
	if input == nil {
		return BuildError("unavailable")
	}
	if _, err := input.Write(append(body, '\n')); err != nil {
		return BuildError("unavailable")
	}
	select {
	case frame := <-pending:
		if frame.Error != "" {
			return BuildError(frame.Error)
		}
		if len(frame.Result) > ReplyBytesLimit || json.Unmarshal(frame.Result, result) != nil {
			return BuildError("runtime_unverified")
		}
		return nil
	case <-s.done:
		return BuildError("unavailable")
	case <-ctx.Done():
		s.stop()
		return ctx.Err()
	}
}
func (b *Builder) Query(ctx context.Context, root, question string) (QueryResult, error) {
	if len(question) == 0 || len(question) > QueryBytesLimit {
		return QueryResult{}, BuildError("bad_request")
	}
	root, err := canonical(root)
	if err != nil {
		return QueryResult{}, err
	}
	if _, _, err = readManifest(root); err != nil {
		return QueryResult{}, BuildError("index_unverified")
	}
	s, err := b.ensureService(ctx, root)
	if err != nil {
		return QueryResult{}, err
	}
	status, err := Inspect(ctx, root)
	if err != nil || status.State != Matched {
		return QueryResult{}, BuildError("source_changed")
	}
	var answer QueryResult
	if err = s.call(ctx, "query", question, &answer); err != nil {
		return QueryResult{}, err
	}
	if !b.enabled() {
		return QueryResult{}, BuildError("disabled")
	}
	after, err := Inspect(ctx, root)
	if err != nil || after.State != Matched || after.Generation != answer.Generation {
		raw, _ := json.Marshal(map[string]any{"before": status, "after": after, "answer_generation": answer.Generation})
		if len(raw) <= BuildLogBytesLimit {
			_ = os.WriteFile(filepath.Join(b.cache, "last-query.log"), raw, 0600)
		}
		return QueryResult{}, BuildError("source_changed")
	}
	return answer, nil
}
