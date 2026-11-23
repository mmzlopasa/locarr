package native

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
	"golang.org/x/text/encoding/charmap"
)

// compileSelector parses a CSS selector (cascadia: a permissively licensed,
// pure-Go implementation over golang.org/x/net/html).
func compileSelector(s string) (cascadia.SelectorGroup, error) {
	return cascadia.ParseGroup(s)
}

// parseHTML parses an HTML page.
func parseHTML(body []byte) (*html.Node, error) {
	return html.Parse(bytes.NewReader(body))
}

// parseXML reads an XML document (RSS, Atom) into the same node tree as an
// HTML page, so that the same selectors apply. Elements keep their local
// name only (nyaa:seeders becomes "seeders"); CDATA becomes text. The
// decoder never expands DTD entities.
func parseXML(body []byte) (*html.Node, error) {
	d := xml.NewDecoder(bytes.NewReader(body))
	d.Strict = false
	d.Entity = xml.HTMLEntity
	d.CharsetReader = func(label string, in io.Reader) (io.Reader, error) {
		switch strings.ToLower(strings.TrimSpace(label)) {
		case "iso-8859-1", "latin1", "latin-1":
			return charmap.ISO8859_1.NewDecoder().Reader(in), nil
		case "windows-1252", "cp1252":
			return charmap.Windows1252.NewDecoder().Reader(in), nil
		}
		return in, nil
	}
	root := &html.Node{Type: html.DocumentNode}
	cur := root
	depth := 0
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if depth == 0 && root.FirstChild == nil {
				return nil, err
			}
			break // keep what was read from a truncated document
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &html.Node{Type: html.ElementNode, Data: strings.ToLower(t.Name.Local)}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
					continue
				}
				n.Attr = append(n.Attr, html.Attribute{Key: strings.ToLower(a.Name.Local), Val: a.Value})
			}
			cur.AppendChild(n)
			cur = n
			depth++
		case xml.EndElement:
			if cur.Parent != nil {
				cur = cur.Parent
			}
			depth--
		case xml.CharData:
			if len(bytes.TrimSpace(t)) > 0 {
				cur.AppendChild(&html.Node{Type: html.TextNode, Data: string(t)})
			}
		}
	}
	return root, nil
}

// nodeText is the trimmed text content of n.
func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

func attr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val, true
		}
	}
	return "", false
}

// selectOne returns the first match of sel under n (n itself for "").
func selectOne(n *html.Node, sel string) *html.Node {
	if sel == "" {
		return n
	}
	g, err := compileSelector(sel)
	if err != nil {
		return nil
	}
	return cascadia.Query(n, g)
}

func selectAll(n *html.Node, sel string) []*html.Node {
	g, err := compileSelector(sel)
	if err != nil {
		return nil
	}
	return cascadia.QueryAll(n, g)
}

// decodeJSON parses a JSON document keeping numbers exact.
func decodeJSON(body []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// jsonPath walks a dotted path ("data.movies", "torrents.0.hash"); an empty
// path is the value itself.
func jsonPath(v any, path string) (any, bool) {
	if path == "" {
		return v, true
	}
	for _, p := range strings.Split(path, ".") {
		switch x := v.(type) {
		case map[string]any:
			nv, ok := x[p]
			if !ok {
				return nil, false
			}
			v = nv
		case []any:
			i, err := strconv.Atoi(p)
			if err != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, true
}

// jsonString turns a scalar into text ("" for null, objects and arrays).
func jsonString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

// jsonArray returns v as an array (an object's values when the site keys
// results by name, as some APIs do).
func jsonArray(v any) ([]any, bool) {
	switch x := v.(type) {
	case []any:
		return x, true
	case map[string]any:
		out := make([]any, 0, len(x))
		for _, k := range sortedKeys(x) {
			out = append(out, x[k])
		}
		return out, true
	}
	return nil, false
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Stable order for tests and pagination; sites key by title or id.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// row is one result row, from markup or JSON.
type row struct {
	node   *html.Node
	json   any
	parent any // json subrows
}

// raw reads a field's raw text from the row before regex/replace.
func (r row) raw(f Field) (string, error) {
	if r.node != nil {
		n := selectOne(r.node, f.Select)
		if n == nil {
			return "", nil
		}
		if f.Attr != "" {
			v, _ := attr(n, f.Attr)
			return strings.TrimSpace(v), nil
		}
		return nodeText(n), nil
	}
	src, path := r.json, f.Select
	if strings.HasPrefix(path, "parent.") {
		src, path = r.parent, strings.TrimPrefix(path, "parent.")
	}
	v, ok := jsonPath(src, path)
	if !ok {
		return "", nil
	}
	if arr, isArr := v.([]any); isArr {
		parts := make([]string, 0, len(arr))
		for _, e := range arr {
			if s := jsonString(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ","), nil
	}
	return strings.TrimSpace(jsonString(v)), nil
}

func (r row) String() string {
	if r.node != nil {
		return fmt.Sprintf("<%s>", r.node.Data)
	}
	return "json row"
}
