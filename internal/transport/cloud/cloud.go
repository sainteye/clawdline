// Package cloud is the inbound adapter for app.clawdline.com: the seam between
// a relay connection and this daemon's answer to what came over it.
//
// It owns two small things and no policy. The first is the shape of that seam
// — a decrypted request in, a payload and the channel it belongs on out —
// stated as an interface so the connection half (internal/adapters/cloud) and
// the operation half (internal/app/cloudops) can be built, tested and replaced
// apart from each other. The second is the router that lets a Cloud request be
// answered by the route a paired browser on this machine's own network would
// have used, in this process, without a socket.
//
// Nothing here decides what an operation means. That is cloudops, and the
// reason it is elsewhere is that deciding it needs no relay at all.
package cloud

import (
	"context"
	"log"

	"github.com/sainteye/clawdline/internal/app/cloudops"
	"github.com/sainteye/clawdline/internal/domain/cloud"
)

// Inbound is one decrypted request, as the transport hands it over. It is the
// envelope's identity fields and its plaintext, and deliberately not the
// envelope: by this point the signature has been checked and the ciphertext
// opened, and repeating either here would be a second opinion about a question
// already answered.
type Inbound struct {
	Channel   string
	Class     string
	Sender    string
	Sequence  uint64
	Plaintext []byte
}

// Outbound is one answer, ready to seal: the channel it belongs on, the class
// that channel allows, and the bytes.
type Outbound struct {
	Channel string
	Class   string
	Payload []byte
	// Reply names the request this answers, for the transport's own log and
	// for the receipt it may correlate. It is not part of the payload.
	Reply Reply
	// Refusal marks the small typed answer that tells a waiter its channel is
	// full. It is the one publication admitted past that channel's own caps
	// (`Spool.ReserveRefusal`), because everything else about a full channel
	// is exactly what cannot get through it.
	Refusal bool
}

// Reply is who an answer is for, which the payload itself does not say.
type Reply struct {
	Sender   string
	Sequence uint64
	// Name is the payload's `read`, repeated here so a log line can carry it
	// without parsing the ciphertext back.
	Name string
	// Status and Code are the answer's own, for the same reason.
	Status int
	Code   string
}

// Transport is the half this package does not implement: where requests arrive
// from and where answers go. A relay connection is one implementation; the
// fake below is the other, and the tests are written against the fake because
// a bridge that needs a relay to be tested is a bridge nobody tests.
type Transport interface {
	// Requests is closed when the transport is finished. A closed channel ends
	// Service.Run, which is how a connection that went away stops this loop
	// rather than leaving it spinning.
	Requests() <-chan Inbound
	// Publish seals and sends one answer. An error is the transport's own —
	// offline, refused, too large — and never a reason to answer differently:
	// the answer was already decided.
	Publish(ctx context.Context, out Outbound) error
}

// Service pumps one transport through one bridge.
//
// It is the whole wiring, and it is this short on purpose: every decision
// worth arguing about is in cloudops, and everything about keys and sockets is
// in the transport. What is left is the address arithmetic — which channel an
// answer goes on — and the honesty about what could not be delivered.
type Service struct {
	// MachineID is this machine's Cloud id: the `ctl/<machine>` it answers on
	// and the first segment of every answer channel.
	MachineID string
	Bridge    cloudops.Bridge
	Transport Transport
	// Log is where a refusal that reached nobody is recorded. Nil logs through
	// the standard logger, because a silent drop here is the failure this
	// whole shape exists to prevent.
	Log func(format string, args ...any)
}

// AnswerChannel is where an answer to a request about `session` is published.
//
// `t/<machine>/<session>` is the transcript channel, which the relay has
// carried and the viewer has subscribed to all along. A read's answer rides it
// rather than a new prefix because a prefix is the one part of an envelope the
// relay reads, and adding one would need a relay this repository does not
// contain. The payload says which read it is, which costs the relay nothing:
// everything past `ch` is ciphertext to it.
func AnswerChannel(machine, session string) string {
	return "t/" + cloudops.ChannelSegment(machine) + "/" + cloudops.ChannelSegment(session)
}

// Congested is a transport that can tell "this channel is full" from every
// other reason an answer did not leave, and say what that channel's bound is.
//
// It is an interface and not an error comparison in Service because the fact
// belongs to whoever owns the spool: the relay knows that a reservation was
// refused at a capacity cap, a fake knows what it was told to fail with, and
// Service knows only that Publish said no. What Service does with the answer
// is the same either way — the person waiting is told, rather than left to
// find out in sixty seconds that nothing is coming.
type Congested interface {
	// ChannelFull reports whether this error is a channel at its cap, as
	// opposed to a line that is down or an envelope that would not seal.
	ChannelFull(err error) bool
	// ChannelByteLimit is what one channel may have owed to it, which the
	// refusal's detail carries so the person is told a number and not an
	// adjective.
	ChannelByteLimit() int
}

// Refuser is a transport whose queue can turn a request away (Relay). Service
// answers each one it turned away, so the person who sent it hears "busy"
// rather than nothing (limits N20).
type Refuser interface {
	// Refused is closed when the transport is finished, as Requests is.
	Refused() <-chan Inbound
	// QueueDepth is what a refusal's detail reports as the limit.
	QueueDepth() int
	// Unanswered records a refused request whose sender could not be told.
	Unanswered(in Inbound, why string)
}

// Run answers requests until the transport's channel closes or ctx is done.
//
// Refusals are answered on a goroutine of their own. The queue is full exactly
// when the bridge is slow, and a busy answer that waited behind the slow
// request would reach its sender when there was no longer anything to be busy
// about.
func (s Service) Run(ctx context.Context) error {
	if r, ok := s.Transport.(Refuser); ok {
		go s.refuse(ctx, r)
	}
	requests := s.Transport.Requests()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case request, open := <-requests:
			if !open {
				return nil
			}
			s.Answer(ctx, request)
		}
	}
}

// Answer handles one request and publishes what it decided.
//
// A request whose answer has no channel is not a failure and not a silence: it
// is the case where the body named no waiter this machine may safely settle,
// and the refusal is recorded here instead of being published somewhere a
// different reader would receive it.
func (s Service) Answer(ctx context.Context, request Inbound) cloudops.Answer {
	answer := s.Bridge.Handle(ctx, cloudops.Command{
		Channel:   request.Channel,
		Class:     cloudops.Class(request.Class),
		Sender:    request.Sender,
		Sequence:  request.Sequence,
		Plaintext: request.Plaintext,
	})
	if !answer.Published() {
		s.logf("cloud: %s answered nobody: session=%s status=%d code=%s sender=%s seq=%d",
			answer.Name, subjectOrUnknown(answer.Subject), answer.Status, answer.Code,
			request.Sender, request.Sequence)
		return answer
	}
	channel := AnswerChannel(s.MachineID, answer.Session)
	// The one channel rule this side has to keep: a build may read more
	// channels than it may write, and publishing on one it has not been
	// cleared for is how a reader nobody deployed starts receiving envelopes.
	if err := cloud.ProducibleChannel(channel); err != nil {
		s.logf("cloud: %s could not be published: %v", answer.Name, err)
		return answer
	}
	out := Outbound{Channel: channel, Class: string(cloud.ClassStream), Payload: answer.Payload,
		Reply: Reply{Sender: request.Sender, Sequence: request.Sequence, Name: answer.Name,
			Status: answer.Status, Code: answer.Code}}
	if err := s.Transport.Publish(ctx, out); err != nil {
		// The answer's own channel is the only way back to the asker, so a
		// publication that cannot leave is never retried into a second
		// effect. But a full channel is not a reason to say nothing: the
		// answer is what will not fit, and the sentence saying so will, under
		// the reserve. Measured on 2026-09-21: a transcript channel refused
		// 34 answers over one evening, each of them one log line on this
		// machine and sixty seconds of "loading" on somebody's phone.
		if c, ok := s.Transport.(Congested); ok && c.ChannelFull(err) {
			s.tellChannelFull(ctx, request, answer, c.ChannelByteLimit(), err)
			return answer
		}
		s.logf("cloud: %s was not delivered: %v", answer.Name, err)
	}
	return answer
}

// tellChannelFull publishes the typed refusal that says a channel is full, on
// that same channel, and records it when even that could not go.
//
// The refusal is deliberately built from the request rather than from the
// answer: whether the sender may be told "busy" or must be told "your command
// ran and the reply is lost" is a fact about what was asked, and the answer
// that will not fit is not evidence about it (Bridge.Undeliverable).
func (s Service) tellChannelFull(ctx context.Context, request Inbound, answer cloudops.Answer, limit int, cause error) {
	refusal := s.Bridge.Undeliverable(cloudops.Command{
		Channel:   request.Channel,
		Class:     cloudops.Class(request.Class),
		Sender:    request.Sender,
		Sequence:  request.Sequence,
		Plaintext: request.Plaintext,
	}, limit)
	if !refusal.Published() {
		s.logf("cloud: %s did not fit its channel and its sender could not be told: the request names no waiter (%s): %v",
			answer.Name, refusal.Code, cause)
		return
	}
	channel := AnswerChannel(s.MachineID, refusal.Session)
	if err := cloud.ProducibleChannel(channel); err != nil {
		s.logf("cloud: %s did not fit its channel and its sender could not be told: %v", answer.Name, err)
		return
	}
	out := Outbound{Channel: channel, Class: string(cloud.ClassStream), Payload: refusal.Payload,
		Refusal: true,
		Reply: Reply{Sender: request.Sender, Sequence: request.Sequence, Name: refusal.Name,
			Status: refusal.Status, Code: refusal.Code}}
	if err := s.Transport.Publish(ctx, out); err != nil {
		s.logf("cloud: %s did not fit its channel and the refusal did not either: %v (channel: %v)",
			answer.Name, err, cause)
		return
	}
	s.logf("cloud: %s did not fit its channel and its sender was told %s (%d): %v",
		answer.Name, refusal.Code, refusal.Status, cause)
}

// refuse tells every request the transport turned away that it was.
func (s Service) refuse(ctx context.Context, r Refuser) {
	refused := r.Refused()
	for {
		select {
		case <-ctx.Done():
			return
		case request, open := <-refused:
			if !open {
				return
			}
			if why := s.Busy(ctx, request, r.QueueDepth()); why != "" {
				r.Unanswered(request, why)
			}
		}
	}
}

// Busy answers one request the queue turned away with `cloud_ingress_busy`,
// on the channel its sender waits on. It returns why the sender could not be
// told, or "" when the answer was handed to the transport.
func (s Service) Busy(ctx context.Context, request Inbound, limit int) string {
	answer := s.Bridge.Busy(cloudops.Command{
		Channel:   request.Channel,
		Class:     cloudops.Class(request.Class),
		Sender:    request.Sender,
		Sequence:  request.Sequence,
		Plaintext: request.Plaintext,
	}, limit)
	if !answer.Published() {
		return "the request names no waiter a refusal may be published to (" + answer.Code + ")"
	}
	channel := AnswerChannel(s.MachineID, answer.Session)
	if err := cloud.ProducibleChannel(channel); err != nil {
		return err.Error()
	}
	out := Outbound{Channel: channel, Class: string(cloud.ClassStream), Payload: answer.Payload,
		Reply: Reply{Sender: request.Sender, Sequence: request.Sequence, Name: answer.Name,
			Status: answer.Status, Code: answer.Code}}
	if err := s.Transport.Publish(ctx, out); err != nil {
		return "the refusal could not be sent: " + err.Error()
	}
	return ""
}

func (s Service) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log(format, args...)
		return
	}
	log.Printf(format, args...)
}

// subjectOrUnknown is what the log says a silent answer was about. A body too
// malformed to name a session reads as `unknown` rather than as a gap in the
// line, because a gap there is indistinguishable from a session called "".
func subjectOrUnknown(subject string) string {
	if subject == "" {
		return "unknown"
	}
	return subject
}
