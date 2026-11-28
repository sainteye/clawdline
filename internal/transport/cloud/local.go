package cloud

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"io"
	"net/http"
	"net/url"
	"strings"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/app/cloudops"
)

// Router answers a Cloud operation out of this daemon's own routes, in this
// process.
//
// **Not an HTTP call to itself.** The handler is invoked directly, so there is
// no second socket, no second TLS decision and no port that has to be open for
// the machine to answer its own questions. What it does keep is everything
// behind that handler: the gate, the capability each route needs, idempotency,
// queueing, validation and the exact refusal words — one implementation of all
// of it, shared with the paired browser on this machine's own network. A
// second router would be a second set of permission checks, and the second one
// is the one that goes wrong.
type Router struct {
	// Handler is the daemon's own handler, gate and all.
	Handler http.Handler
	// Authorize stamps each request with the credential the gate judges it by.
	//
	// Nil is deliberate and is not a convenience: with no credential the gate
	// answers `unauthorized`, which is the correct answer for a daemon that
	// has not been told who this Cloud viewer is. Whoever wires this decides
	// what a paired viewer may do here, and that decision is theirs to write
	// down rather than this file's to assume.
	Authorize func(r *http.Request)
	// Host is the Host header these requests carry. Empty is 127.0.0.1, which
	// is what the gate's rebinding check accepts and what an in-process
	// request truthfully is.
	Host string
	// AppOrigin is the hosted console the viewer being answered is looking at,
	// `cloud_app_origin`. It reaches the routes as a context value rather than
	// a header, and is the answer to a different question than the Origin the
	// authorizer stamps — see WithAppOrigin, which says which is which.
	//
	// Empty is a router that cannot say, and a route that wanted it then reads
	// the request's own Origin as it always did.
	AppOrigin string
	// ViewerGrant rechecks a verified viewer at the destination route.
	ViewerGrant func(context.Context, string, string, ed25519.PublicKey) (adaptercloud.AssistantViewerGrant, error)
}

// Do dispatches one request and returns what the route answered.
func (r Router) Do(ctx context.Context, req cloudops.LocalRequest) (cloudops.LocalResponse, error) {
	if r.Handler == nil {
		return cloudops.LocalResponse{}, errNoHandler
	}
	target := req.Path
	if len(req.Query) > 0 {
		values := url.Values{}
		for key, value := range req.Query {
			values.Set(key, value)
		}
		target += "?" + values.Encode()
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	// The path is already escaped where it needed to be — a tmux pane is
	// `%19` — so the target is parsed rather than re-encoded, and the
	// original spelling is what the routes split on.
	parsed, err := url.ParseRequestURI(target)
	if err != nil {
		return cloudops.LocalResponse{}, routerError("that is not a route: " + target)
	}
	host := r.Host
	if host == "" {
		host = "127.0.0.1"
	}
	request := (&http.Request{
		Method: method, URL: parsed, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{}, Host: host, RequestURI: target,
		Body:          io.NopCloser(bytes.NewReader(req.Body)),
		ContentLength: int64(len(req.Body)),
	}).WithContext(withCloudViewer(WithAppOrigin(withViaCloud(ctx), r.AppOrigin), req.Viewer))
	if req.Viewer != "" && r.ViewerGrant != nil {
		grant := func(ctx context.Context, account string) (adaptercloud.AssistantViewerGrant, error) {
			return r.ViewerGrant(ctx, account, req.Viewer, req.ViewerKey)
		}
		request = request.WithContext(context.WithValue(request.Context(), viewerGrantKey{}, grant))
	}
	if len(req.Body) > 0 {
		// The gate refuses a change whose body is neither JSON nor nothing,
		// because a page elsewhere can post a form without asking and cannot
		// post JSON without this daemon agreeing first.
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range req.Header {
		request.Header.Set(key, value)
	}
	if r.Authorize != nil {
		r.Authorize(request)
	}
	out := &recorder{header: http.Header{}}
	r.Handler.ServeHTTP(out, request)
	return cloudops.LocalResponse{
		Status:         out.statusOr(http.StatusOK),
		Body:           out.body.Bytes(),
		ContentType:    mediaType(out.header.Get("Content-Type")),
		FixedDetailKey: out.fixedDetailKey,
	}, nil
}

// viaCloudKey marks a request this Router built.
type viaCloudKey struct{}

// withViaCloud marks ctx as a request that came in through the Cloud line.
// Do marks every request, unconditionally: it is not inferred from the app
// origin, which WithAppOrigin leaves unset when it is empty, so a router
// without one would otherwise send requests that look like they arrived over
// a socket.
func withViaCloud(ctx context.Context) context.Context {
	return context.WithValue(ctx, viaCloudKey{}, true)
}

// ViaCloud is whether this request was built by a Router: a Cloud viewer's,
// answered in process with this machine's own credentials. A route that must
// never be carried over Cloud — the terminals — refuses on it.
//
// Like the app origin it is a context value and not a header, and for the
// same reason: net/http builds a socket request's context itself, so nothing
// arriving over a socket can carry it, and nothing can take it off a request
// this package built.
func ViaCloud(ctx context.Context) bool {
	via, _ := ctx.Value(viaCloudKey{}).(bool)
	return via
}

// cloudViewerKey carries the Cloud viewer a request was dispatched for.
type cloudViewerKey struct{}

// withCloudViewer marks ctx as answered on behalf of `viewer`, the envelope's
// verified sender. Empty leaves ctx unmarked.
func withCloudViewer(ctx context.Context, viewer string) context.Context {
	if viewer == "" {
		return ctx
	}
	return context.WithValue(ctx, cloudViewerKey{}, viewer)
}

// CloudViewerOf is the Cloud viewer device this request is answered on behalf
// of, or "" for one that did not come from a viewer: every request over a
// socket, and this machine's own in-process reads.
//
// **It is what tells two phones apart here.** Every viewer reaches the routes
// holding this machine's own local credential (`LocalAuthorizer`), so the
// gate's verdict names the same device for all of them. The sender of a
// verified envelope does not: the bridge has already checked its signature
// and the account's roster before it is carried. A route that owns something
// per device — a push subscription, an idempotency receipt — owns it by this.
//
// A context value and not a header, for the reason the app origin is one: a
// header naming a viewer would be a header that grants, and nothing arriving
// over a socket can put a value into a request's context.
func CloudViewerOf(ctx context.Context) string {
	viewer, _ := ctx.Value(cloudViewerKey{}).(string)
	return viewer
}

type viewerGrantKey struct{}

// CloudViewerGrant rechecks the transport's authenticated viewer. A socket
// request cannot provide this proof through any header or request field.
func CloudViewerGrant(ctx context.Context, account string) (adaptercloud.AssistantViewerGrant, error) {
	resolve, _ := ctx.Value(viewerGrantKey{}).(func(context.Context, string) (adaptercloud.AssistantViewerGrant, error))
	if resolve == nil {
		return adaptercloud.AssistantViewerGrant{}, adaptercloud.ErrViewerNotPinned
	}
	return resolve(ctx, account)
}

// recorder is the ResponseWriter an in-process dispatch answers into. It keeps
// what a handler wrote and nothing else: no flushing, no hijacking, no
// trailers — a Cloud answer is one payload, decided and then sealed.
type recorder struct {
	header         http.Header
	status         int
	body           bytes.Buffer
	fixedDetailKey string
}

// ClawdlineFixedRefusalKey is called only by this daemon's fixed refusal
// writers; it does not read a header or the response body.
func (r *recorder) ClawdlineFixedRefusalKey(key string) { r.fixedDetailKey = key }

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(p)
}

func (r *recorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

// statusOr is what a handler that wrote nothing at all answered: Go's own
// server would send 200 with an empty body, and reporting anything else here
// would invent a refusal the route did not make.
func (r *recorder) statusOr(fallback int) int {
	if r.status == 0 {
		return fallback
	}
	return r.status
}

// mediaType keeps the header as written, because the two answers that are not
// JSON are told apart by their exact spelling — `image/png`, and a document's
// `text/markdown; charset=utf-8` — and a parse that dropped the parameter
// would make a Markdown file and a Markdown file in another encoding the same
// fact.
func mediaType(header string) string { return strings.TrimSpace(header) }

type routerError string

func (e routerError) Error() string { return string(e) }

const errNoHandler = routerError("this Cloud router was built without a handler")
