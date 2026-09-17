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

	"github.com/sainteye/clawdline-go/internal/app/cloudops"
	"github.com/sainteye/clawdline-go/internal/domain/cloud"
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

// Run answers requests until the transport's channel closes or ctx is done.
func (s Service) Run(ctx context.Context) error {
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
		s.logf("cloud: %s answered nobody: status=%d code=%s sender=%s seq=%d",
			answer.Name, answer.Status, answer.Code, request.Sender, request.Sequence)
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
		// publication that cannot leave is recorded rather than retried into a
		// second effect.
		s.logf("cloud: %s was not delivered: %v", answer.Name, err)
	}
	return answer
}

func (s Service) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log(format, args...)
		return
	}
	log.Printf(format, args...)
}
