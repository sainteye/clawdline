package turnreport

import (
	"bytes"
	"html"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// markdown renders a document the report quotes into HTML that cannot act.
//
// The quoted file is somebody's text, not the report's code. Raw HTML in it —
// a `<script>`, an `<img onerror>` — is shown as the characters it is rather
// than dropped, so the reader still sees what the file says. An image is shown
// as a link to where it points and never loaded: a report opened from disk
// must not fetch anything, and a relative `src` would read a file beside the
// report. goldmark's own renderer already refuses `javascript:` and the other
// dangerous link targets when it is not told to be unsafe, which it is not.
var markdown = goldmark.New(
	goldmark.WithExtensions(extension.Table, extension.Strikethrough),
	goldmark.WithRendererOptions(
		renderer.WithNodeRenderers(util.Prioritized(inertHTML{}, 100)),
	),
)

// renderMarkdown answers the HTML for one Markdown text.
func renderMarkdown(src []byte) (string, error) {
	var out bytes.Buffer
	if err := markdown.Convert(src, &out); err != nil {
		return "", err
	}
	return out.String(), nil
}

// inertHTML replaces goldmark's renderers for the three nodes that could make
// a quoted file do something.
type inertHTML struct{}

func (inertHTML) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindHTMLBlock, renderHTMLBlock)
	reg.Register(ast.KindRawHTML, renderRawHTML)
	reg.Register(ast.KindImage, renderImage)
}

func renderHTMLBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.HTMLBlock)
	var text bytes.Buffer
	for i := range n.Lines().Len() {
		line := n.Lines().At(i)
		text.Write(line.Value(source))
	}
	if n.HasClosure() {
		text.Write(n.ClosureLine.Value(source))
	}
	_, _ = w.WriteString(`<pre class="rawhtml"><code>`)
	_, _ = w.WriteString(html.EscapeString(text.String()))
	_, _ = w.WriteString("</code></pre>\n")
	return ast.WalkSkipChildren, nil
}

func renderRawHTML(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkSkipChildren, nil
	}
	n := node.(*ast.RawHTML)
	_, _ = w.WriteString("<code>")
	for i := range n.Segments.Len() {
		seg := n.Segments.At(i)
		_, _ = w.WriteString(html.EscapeString(string(seg.Value(source))))
	}
	_, _ = w.WriteString("</code>")
	return ast.WalkSkipChildren, nil
}

// renderImage writes `[image: alt]` as a link to the picture's address, when
// that address is not a dangerous one, and as plain text otherwise.
func renderImage(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Image)
	alt := html.EscapeString(string(imageAlt(n, source)))
	dest := util.URLEscape(n.Destination, true)
	if len(dest) > 0 && !gmhtml.IsDangerousURL(dest) {
		_, _ = w.WriteString(`<a class="img" href="`)
		_, _ = w.Write(util.EscapeHTML(dest))
		_, _ = w.WriteString(`">[image: ` + alt + `]</a>`)
	} else {
		_, _ = w.WriteString(`<span class="img">[image: ` + alt + `]</span>`)
	}
	return ast.WalkSkipChildren, nil
}

func imageAlt(n ast.Node, source []byte) []byte {
	var out []byte
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch t := c.(type) {
		case *ast.Text:
			out = append(out, t.Segment.Value(source)...)
		case *ast.String:
			out = append(out, t.Value...)
		default:
			out = append(out, imageAlt(c, source)...)
		}
	}
	return out
}
