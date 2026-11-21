package customformats

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/languages"
)

// Flavor picks the numbering of enumerated values in shared JSON: the
// movie-oriented and the series-oriented tools number sources differently.
type Flavor string

// Flavors.
const (
	FlavorMovies Flavor = "movies"
	FlavorSeries Flavor = "series"
)

var movieSources = []string{"unknown", "cam", "telesync", "telecine", "workprint", "dvd", "tv", "webdl", "webrip", "bluray"}
var seriesSources = []string{"unknown", "tv", "tvraw", "webdl", "webrip", "dvd", "bluray", "blurayraw"}

// shared modifiers numbering (index = number)
var sharedModifiers = []string{"none", "regional", "screener", "rawhd", "brdisk", "remux"}

// series release types numbering
var sharedReleaseTypes = []string{"unknown", "single", "multi", "season"}

// indexer flags are bit values in shared JSON
var sharedFlags = map[int]string{1: "freeleech", 2: "halfleech", 4: "double_upload", 8: "golden", 16: "approved",
	32: "internal", 128: "scene", 256: "freeleech75", 512: "freeleech25", 2048: "nuked"}

type sharedSpec struct {
	Name           string          `json:"name"`
	Implementation string          `json:"implementation"`
	Negate         bool            `json:"negate"`
	Required       bool            `json:"required"`
	Fields         json.RawMessage `json:"fields"`
}

type sharedFormat struct {
	Name                string       `json:"name"`
	IncludeWhenRenaming bool         `json:"includeCustomFormatWhenRenaming"`
	Specifications      []sharedSpec `json:"specifications"`
}

// ImportResult is the outcome of an import.
type ImportResult struct {
	Formats  []Format
	Warnings []string
}

// Import reads one format (object) or several (array) in the shared JSON
// shape. Fields may be an object ({"value": …}) or the older list form
// ([{"name": "value", "value": …}]). Unknown implementations are kept and
// reported: an unknown optional condition is ignored, an unknown required
// one keeps the format from ever applying.
func Import(data []byte, flavor Flavor) (ImportResult, error) {
	var res ImportResult
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return res, invalid("paste the JSON of one or more custom formats")
	}
	var list []sharedFormat
	if data[0] == '[' {
		if err := json.Unmarshal(data, &list); err != nil {
			return res, invalid("not valid custom-format JSON: %v", err)
		}
	} else {
		var one sharedFormat
		if err := json.Unmarshal(data, &one); err != nil {
			return res, invalid("not valid custom-format JSON: %v", err)
		}
		list = []sharedFormat{one}
	}
	if len(list) > 500 {
		return res, invalid("at most 500 formats at once")
	}
	for i, sf := range list {
		name := strings.TrimSpace(sf.Name)
		if name == "" {
			return res, invalid("format %d has no name", i+1)
		}
		f := Format{Name: name, IncludeWhenRenaming: sf.IncludeWhenRenaming}
		for _, ss := range sf.Specifications {
			fields, err := decodeFields(ss.Fields)
			if err != nil {
				return res, invalid("%s: condition %q: %v", name, ss.Name, err)
			}
			s := Spec{Name: ss.Name, Implementation: ss.Implementation, Negate: ss.Negate, Required: ss.Required, Fields: fields}
			if !supported(s.Implementation) {
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s: condition %q uses %s, which Locarr does not know; it is kept but ignored (or, if required, the format never applies)",
					name, ss.Name, ss.Implementation))
			} else if w := fromShared(&s, flavor); w != "" {
				res.Warnings = append(res.Warnings, name+": "+w)
			}
			f.Specs = append(f.Specs, s)
		}
		if _, err := Compile(f); err != nil {
			return res, err
		}
		res.Formats = append(res.Formats, f)
	}
	return res, nil
}

func decodeFields(raw json.RawMessage) (map[string]any, error) {
	out := map[string]any{}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	if raw[0] == '[' {
		var list []struct {
			Name  string `json:"name"`
			Value any    `json:"value"`
		}
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
		for _, f := range list {
			out[f.Name] = f.Value
		}
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func indexName(list []string, v any) (string, bool) {
	n, ok := asNumber(v)
	if !ok || n < 0 || int(n) >= len(list) || n != float64(int(n)) {
		return "", false
	}
	return list[int(n)], true
}

// fromShared converts shared numeric values to Locarr's names.
func fromShared(s *Spec, flavor Flavor) string {
	v := s.Value()
	if _, isNum := v.(float64); !isNum {
		return ""
	}
	switch s.Implementation {
	case Source:
		list := movieSources
		if flavor == FlavorSeries {
			list = seriesSources
		}
		if name, ok := indexName(list, v); ok {
			s.Fields["value"] = name
			return ""
		}
	case QualityModifier:
		if name, ok := indexName(sharedModifiers, v); ok {
			s.Fields["value"] = name
			return ""
		}
	case ReleaseType:
		if name, ok := indexName(sharedReleaseTypes, v); ok {
			s.Fields["value"] = name
			return ""
		}
	case Language:
		if code, ok := languages.FromLegacyID(int(v.(float64))); ok {
			s.Fields["value"] = code
			return ""
		}
	case IndexerFlag:
		if name, ok := sharedFlags[int(v.(float64))]; ok {
			s.Fields["value"] = name
			return ""
		}
	default:
		return ""
	}
	return fmt.Sprintf("condition %q: value %v is not known", s.Name, v)
}

// Export writes a format in the shared JSON shape (numeric values in the
// given flavor's numbering where the shared format uses numbers).
func Export(f Format, flavor Flavor) map[string]any {
	specs := make([]map[string]any, 0, len(f.Specs))
	for _, s := range f.Specs {
		fields := map[string]any{}
		for k, v := range s.Fields {
			fields[k] = v
		}
		if str, ok := s.Value().(string); ok {
			switch s.Implementation {
			case Source:
				list := movieSources
				if flavor == FlavorSeries {
					list = seriesSources
				}
				if i := indexOf(list, str); i >= 0 {
					fields["value"] = i
				}
			case QualityModifier:
				if i := indexOf(sharedModifiers, str); i >= 0 {
					fields["value"] = i
				}
			case ReleaseType:
				if i := indexOf(sharedReleaseTypes, str); i >= 0 {
					fields["value"] = i
				}
			case Language:
				if id, ok := languages.LegacyID(str); ok {
					fields["value"] = id
				}
			case IndexerFlag:
				for bit, name := range sharedFlags {
					if name == str {
						fields["value"] = bit
					}
				}
			}
		}
		specs = append(specs, map[string]any{"name": s.Name, "implementation": s.Implementation, "negate": s.Negate,
			"required": s.Required, "fields": fields})
	}
	return map[string]any{"name": f.Name, "includeCustomFormatWhenRenaming": f.IncludeWhenRenaming, "specifications": specs}
}

func indexOf(list []string, v string) int {
	for i, x := range list {
		if strings.EqualFold(x, v) {
			return i
		}
	}
	return -1
}
