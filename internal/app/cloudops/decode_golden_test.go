package cloudops

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/cloudvocab"
)

// decoderGolden is what the hand-written decoders answered, before they were
// removed, for every body the equivalence test drove them with: each body the
// vocabulary table lists, each one field away from one (a field dropped, or a
// field added from every key the table or a hand-written key-set check
// spelled, and one nothing spelled), and each with one value swapped for a
// probe at and past every stated limit. The generated decoders are held to
// it, so the behaviour they replaced stays pinned without the code.
//
// One JSON line per body, gzipped: the file is large, and only this test
// reads it. CLAWDLINE_DECODER_GOLDEN=record rewrites it from the generated
// decoders, for a table change that is meant to change what a word accepts;
// the diff of the decompressed lines is then the review.
const decoderGolden = "testdata/cloud_decoders.golden.jsonl.gz"

type goldenCase struct {
	Op      string          `json:"op"`
	Body    json.RawMessage `json:"body"`
	Accept  bool            `json:"accept"`
	Plan    string          `json:"plan,omitempty"`
	Request string          `json:"request,omitempty"`
}

// served is the decoder the bridge runs for o: its decode, then its route
// when it has one.
func served(o op) genDecoder {
	return func(b body) (plan, LocalRequest, bool) {
		p, ok := o.decode(b)
		if !ok {
			return plan{}, LocalRequest{}, false
		}
		var req LocalRequest
		if o.route != nil {
			req = o.route(p)
		}
		return p, req, true
	}
}

// goldenAnswer is one decoder's decision on b, in the golden's terms.
func goldenAnswer(o op, decode genDecoder, word string, b body) (goldenCase, error) {
	encoded, err := json.Marshal(b)
	if err != nil {
		return goldenCase{}, err
	}
	c := goldenCase{Op: word, Body: encoded}
	p, req, ok := decode(cloneBody(b))
	if ok {
		c.Accept, c.Plan, c.Request = true, planIdentity(o, p), describeRequest(req)
	}
	return c, nil
}

func readDecoderGolden(t *testing.T) []goldenCase {
	t.Helper()
	f, err := os.Open(decoderGolden)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	// A line is as long as its body, and a body probing a picture's limit
	// is megabytes: read values, not lines.
	out := []goldenCase{}
	lines := json.NewDecoder(z)
	for lines.More() {
		var c goldenCase
		if err := lines.Decode(&c); err != nil {
			t.Fatalf("golden line %d: %v", len(out)+1, err)
		}
		out = append(out, c)
	}
	return out
}

// goldenBody decodes a recorded body as the bridge decodes a wire body:
// numbers stay json.Number.
func goldenBody(raw json.RawMessage) (body, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var b body
	if err := d.Decode(&b); err != nil {
		return nil, err
	}
	return b, nil
}

func writeDecoderGolden(t *testing.T, cases []goldenCase) {
	t.Helper()
	sort.SliceStable(cases, func(i, j int) bool { return cases[i].Op < cases[j].Op })
	var buf bytes.Buffer
	z, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		line, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		z.Write(append(line, '\n'))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(decoderGolden, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d cases, %d bytes", len(cases), buf.Len())
}

// Every word with a generated decoder, as the bridge serves it, decides every
// recorded body as the golden says.
func TestGeneratedDecodersKeepTheGolden(t *testing.T) {
	cases := readDecoderGolden(t)
	if os.Getenv("CLAWDLINE_DECODER_GOLDEN") == "record" {
		writeDecoderGolden(t, recordDecoderGolden(t, cases))
		return
	}
	perWord := map[string]int{}
	failures := map[string]int{}
	for _, c := range cases {
		perWord[c.Op]++
		if _, ok := generatedDecoders[c.Op]; !ok {
			t.Errorf("%s: recorded, and no generated decoder has the word", c.Op)
			continue
		}
		b, err := goldenBody(c.Body)
		if err != nil {
			t.Fatalf("%s: %v", c.Op, err)
		}
		got, err := goldenAnswer(catalog[c.Op], served(catalog[c.Op]), c.Op, b)
		if err != nil {
			t.Fatal(err)
		}
		if msg := goldenDiff(got, c); msg != "" {
			failures[c.Op]++
			if failures[c.Op] <= 3 {
				t.Errorf("%s body %s: %s", c.Op, describeBody(b), msg)
			}
		}
	}
	for word, n := range failures {
		if n > 3 {
			t.Errorf("%s: …and %d more recorded bodies decided differently", word, n-3)
		}
	}
	for word := range generatedDecoders {
		if perWord[word] == 0 {
			t.Errorf("%s: generated, and the golden records no body for it", word)
		}
	}
	t.Logf("%d recorded bodies over %d words", len(cases), len(perWord))
}

// recordDecoderGolden is every recorded body, and every probe the table's
// rows give that is not recorded yet, decided by the served decoders. A
// word no longer generated drops out.
func recordDecoderGolden(t *testing.T, recorded []goldenCase) []goldenCase {
	t.Helper()
	// A scoped vocabulary extension must not rewrite decisions for older
	// words. Keep their recorded bytes and ordering, and probe only named words.
	selected := map[string]bool{}
	for _, word := range strings.Split(os.Getenv("CLAWDLINE_DECODER_GOLDEN_WORDS"), ",") {
		if word != "" {
			selected[word] = true
		}
	}
	wanted := func(word string) bool { return len(selected) == 0 || selected[word] }
	ops, err := cloudvocab.Operations()
	if err != nil {
		t.Fatal(err)
	}
	universe, err := bodyKeyUniverse(ops, recorded)
	if err != nil {
		t.Fatal(err)
	}
	examples := map[string]any{}
	for _, row := range ops {
		for key, value := range row.Example {
			if _, ok := examples[key]; !ok {
				examples[key] = value
			}
		}
	}
	bodies := map[string][]body{}
	seen := map[string]bool{}
	keep := func(word string, b body) {
		encoded, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		if key := word + "\x00" + string(encoded); !seen[key] {
			seen[key] = true
			bodies[word] = append(bodies[word], b)
		}
	}
	for _, c := range recorded {
		if !wanted(c.Op) {
			continue
		}
		b, err := goldenBody(c.Body)
		if err != nil {
			t.Fatal(err)
		}
		keep(c.Op, b)
	}
	for _, row := range ops {
		if _, ok := generatedDecoders[row.Op]; ok && wanted(row.Op) {
			for _, b := range decoderProbes(row, universe, examples) {
				keep(row.Op, b)
			}
		}
	}
	out := []goldenCase{}
	for _, c := range recorded {
		if !wanted(c.Op) {
			out = append(out, c)
		}
	}
	for _, row := range ops {
		if !wanted(row.Op) {
			continue
		}
		if _, ok := generatedDecoders[row.Op]; !ok {
			continue
		}
		o := catalog[row.Op]
		for _, b := range bodies[row.Op] {
			c, err := goldenAnswer(o, served(o), row.Op, b)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, c)
		}
	}
	return out
}

func goldenDiff(got, want goldenCase) string {
	if got.Accept != want.Accept {
		return fmt.Sprintf("accepts=%v, the golden says %v", got.Accept, want.Accept)
	}
	if got.Plan != want.Plan {
		return fmt.Sprintf("plan:\n  generated %s\n  golden    %s", got.Plan, want.Plan)
	}
	if got.Request != want.Request {
		return fmt.Sprintf("local request:\n  generated %s\n  golden    %s", got.Request, want.Request)
	}
	return ""
}
