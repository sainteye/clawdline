package cloud

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/sainteye/clawdline-go/internal/app/cloudops"
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
	// `%195` — so the target is parsed rather than re-encoded, and the
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
	}).WithContext(ctx)
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
		Status:      out.statusOr(http.StatusOK),
		Body:        out.body.Bytes(),
		ContentType: mediaType(out.header.Get("Content-Type")),
	}, nil
}

// recorder is the ResponseWriter an in-process dispatch answers into. It keeps
// what a handler wrote and nothing else: no flushing, no hijacking, no
// trailers — a Cloud answer is one payload, decided and then sealed.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

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
