package rtorrent

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// A small XML-RPC codec (http://xmlrpc.com/spec.md): enough for rTorrent's
// interface. Go values map as follows: string, int64 (<i4>/<i8>/<int>), bool,
// float64, []byte (<base64>), []any (<array>), map[string]any (<struct>) and
// nil (<nil/>). Document type declarations are refused, so entity tricks
// cannot expand in the decoder; Go's decoder never resolves external entities.

// maxDepth bounds nesting in responses.
const maxDepth = 32

// errBadXML marks a response that is not XML-RPC.
var errBadXML = errors.New("not an XML-RPC response")

// Fault is an XML-RPC fault response.
type Fault struct {
	Code    int64
	Message string
}

func (f *Fault) Error() string { return fmt.Sprintf("XML-RPC fault %d: %s", f.Code, f.Message) }

// encodeCall renders a methodCall.
func encodeCall(method string, params ...any) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0"?><methodCall><methodName>`)
	if err := xml.EscapeText(&b, []byte(method)); err != nil {
		return nil, err
	}
	b.WriteString(`</methodName><params>`)
	for _, p := range params {
		b.WriteString(`<param>`)
		if err := encodeValue(&b, p, 0); err != nil {
			return nil, err
		}
		b.WriteString(`</param>`)
	}
	b.WriteString(`</params></methodCall>`)
	return b.Bytes(), nil
}

func encodeValue(b *bytes.Buffer, v any, depth int) error {
	if depth > maxDepth {
		return errors.New("xmlrpc: value nested too deeply")
	}
	b.WriteString("<value>")
	switch x := v.(type) {
	case nil:
		b.WriteString("<nil/>")
	case string:
		b.WriteString("<string>")
		if err := xml.EscapeText(b, []byte(x)); err != nil {
			return err
		}
		b.WriteString("</string>")
	case bool:
		if x {
			b.WriteString("<boolean>1</boolean>")
		} else {
			b.WriteString("<boolean>0</boolean>")
		}
	case int:
		writeInt(b, int64(x))
	case int64:
		writeInt(b, x)
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return errors.New("xmlrpc: cannot encode NaN or infinity")
		}
		b.WriteString("<double>" + strconv.FormatFloat(x, 'f', -1, 64) + "</double>")
	case []byte:
		b.WriteString("<base64>" + base64.StdEncoding.EncodeToString(x) + "</base64>")
	case []string:
		b.WriteString("<array><data>")
		for _, e := range x {
			if err := encodeValue(b, e, depth+1); err != nil {
				return err
			}
		}
		b.WriteString("</data></array>")
	case []any:
		b.WriteString("<array><data>")
		for _, e := range x {
			if err := encodeValue(b, e, depth+1); err != nil {
				return err
			}
		}
		b.WriteString("</data></array>")
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("<struct>")
		for _, k := range keys {
			b.WriteString("<member><name>")
			if err := xml.EscapeText(b, []byte(k)); err != nil {
				return err
			}
			b.WriteString("</name>")
			if err := encodeValue(b, x[k], depth+1); err != nil {
				return err
			}
			b.WriteString("</member>")
		}
		b.WriteString("</struct>")
	default:
		return fmt.Errorf("xmlrpc: cannot encode %T", v)
	}
	b.WriteString("</value>")
	return nil
}

func writeInt(b *bytes.Buffer, n int64) {
	if n >= math.MinInt32 && n <= math.MaxInt32 {
		b.WriteString("<i4>" + strconv.FormatInt(n, 10) + "</i4>")
		return
	}
	b.WriteString("<i8>" + strconv.FormatInt(n, 10) + "</i8>")
}

// decodeResponse parses a methodResponse: the single result value, or a *Fault.
func decodeResponse(data []byte) (any, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = true
	root, err := nextStart(d)
	if err != nil || root.Name.Local != "methodResponse" {
		return nil, errBadXML
	}
	el, err := nextStart(d)
	if err != nil {
		return nil, errBadXML
	}
	switch el.Name.Local {
	case "params":
		p, err := nextStart(d)
		if err != nil || p.Name.Local != "param" {
			return nil, errBadXML
		}
		v, err := expectValue(d, 0)
		if err != nil {
			return nil, err
		}
		return v, nil
	case "fault":
		v, err := expectValue(d, 0)
		if err != nil {
			return nil, err
		}
		m, ok := v.(map[string]any)
		if !ok {
			return nil, errBadXML
		}
		f := &Fault{}
		f.Code, _ = m["faultCode"].(int64)
		f.Message, _ = m["faultString"].(string)
		return nil, f
	}
	return nil, errBadXML
}

// token returns the next token, refusing DTDs.
func token(d *xml.Decoder) (xml.Token, error) {
	t, err := d.Token()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errBadXML
		}
		return nil, fmt.Errorf("%w: %w", errBadXML, err)
	}
	if _, ok := t.(xml.Directive); ok {
		return nil, fmt.Errorf("%w: document type declarations are not allowed", errBadXML)
	}
	return t, nil
}

// nextStart skips whitespace, comments and processing instructions to the next start element.
func nextStart(d *xml.Decoder) (xml.StartElement, error) {
	for {
		t, err := token(d)
		if err != nil {
			return xml.StartElement{}, err
		}
		switch x := t.(type) {
		case xml.StartElement:
			return x, nil
		case xml.EndElement:
			return xml.StartElement{}, fmt.Errorf("%w: unexpected </%s>", errBadXML, x.Name.Local)
		case xml.CharData:
			if len(bytes.TrimSpace(x)) > 0 {
				return xml.StartElement{}, fmt.Errorf("%w: unexpected text", errBadXML)
			}
		}
	}
}

// nextStartOrEnd returns the next start element, or ok=false at the end element.
func nextStartOrEnd(d *xml.Decoder) (xml.StartElement, bool, error) {
	for {
		t, err := token(d)
		if err != nil {
			return xml.StartElement{}, false, err
		}
		switch x := t.(type) {
		case xml.StartElement:
			return x, true, nil
		case xml.EndElement:
			return xml.StartElement{}, false, nil
		case xml.CharData:
			if len(bytes.TrimSpace(x)) > 0 {
				return xml.StartElement{}, false, fmt.Errorf("%w: unexpected text", errBadXML)
			}
		}
	}
}

// expectEnd consumes whitespace up to an end element.
func expectEnd(d *xml.Decoder) error {
	_, more, err := nextStartOrEnd(d)
	if err != nil {
		return err
	}
	if more {
		return fmt.Errorf("%w: unexpected element", errBadXML)
	}
	return nil
}

// expectValue reads a <value> element.
func expectValue(d *xml.Decoder, depth int) (any, error) {
	el, err := nextStart(d)
	if err != nil {
		return nil, err
	}
	if el.Name.Local != "value" {
		return nil, fmt.Errorf("%w: want <value>, got <%s>", errBadXML, el.Name.Local)
	}
	return readValue(d, depth)
}

// readValue reads the inside of a <value> whose start was consumed.
func readValue(d *xml.Decoder, depth int) (any, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("%w: nested too deeply", errBadXML)
	}
	var text strings.Builder
	for {
		t, err := token(d)
		if err != nil {
			return nil, err
		}
		switch x := t.(type) {
		case xml.CharData:
			text.Write(x)
		case xml.EndElement:
			// <value>text</value> without a type is a string.
			return text.String(), nil
		case xml.StartElement:
			v, err := readTyped(d, x.Name.Local, depth)
			if err != nil {
				return nil, err
			}
			if err := expectEnd(d); err != nil { // </value>
				return nil, err
			}
			return v, nil
		}
	}
}

// readText reads character data up to the end of the current element.
func readText(d *xml.Decoder) (string, error) {
	var text strings.Builder
	for {
		t, err := token(d)
		if err != nil {
			return "", err
		}
		switch x := t.(type) {
		case xml.CharData:
			text.Write(x)
		case xml.EndElement:
			return text.String(), nil
		case xml.StartElement:
			return "", fmt.Errorf("%w: unexpected <%s>", errBadXML, x.Name.Local)
		}
	}
}

func readTyped(d *xml.Decoder, typ string, depth int) (any, error) {
	switch typ {
	case "string", "dateTime.iso8601":
		return readText(d)
	case "i4", "i8", "int":
		s, err := readText(d)
		if err != nil {
			return nil, err
		}
		n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: bad integer", errBadXML)
		}
		return n, nil
	case "boolean":
		s, err := readText(d)
		if err != nil {
			return nil, err
		}
		switch strings.TrimSpace(s) {
		case "1":
			return true, nil
		case "0":
			return false, nil
		}
		return nil, fmt.Errorf("%w: bad boolean", errBadXML)
	case "double":
		s, err := readText(d)
		if err != nil {
			return nil, err
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return nil, fmt.Errorf("%w: bad double", errBadXML)
		}
		return f, nil
	case "base64":
		s, err := readText(d)
		if err != nil {
			return nil, err
		}
		b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(s), ""))
		if err != nil {
			return nil, fmt.Errorf("%w: bad base64", errBadXML)
		}
		return b, nil
	case "nil":
		return nil, expectEnd(d)
	case "array":
		data, err := nextStart(d)
		if err != nil || data.Name.Local != "data" {
			return nil, fmt.Errorf("%w: <array> without <data>", errBadXML)
		}
		out := []any{}
		for {
			el, more, err := nextStartOrEnd(d)
			if err != nil {
				return nil, err
			}
			if !more { // </data>
				break
			}
			if el.Name.Local != "value" {
				return nil, fmt.Errorf("%w: unexpected <%s> in array", errBadXML, el.Name.Local)
			}
			v, err := readValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, expectEnd(d) // </array>
	case "struct":
		out := map[string]any{}
		for {
			el, more, err := nextStartOrEnd(d)
			if err != nil {
				return nil, err
			}
			if !more { // </struct>
				return out, nil
			}
			if el.Name.Local != "member" {
				return nil, fmt.Errorf("%w: unexpected <%s> in struct", errBadXML, el.Name.Local)
			}
			n, err := nextStart(d)
			if err != nil || n.Name.Local != "name" {
				return nil, fmt.Errorf("%w: member without name", errBadXML)
			}
			name, err := readText(d)
			if err != nil {
				return nil, err
			}
			v, err := expectValue(d, depth+1)
			if err != nil {
				return nil, err
			}
			out[name] = v
			if err := expectEnd(d); err != nil { // </member>
				return nil, err
			}
		}
	}
	return nil, fmt.Errorf("%w: unknown type <%s>", errBadXML, typ)
}
