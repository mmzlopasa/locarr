package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Scene is what a date-based scene release name says: the studio (site)
// before the date, the date, and the words after it (performers and title),
// with the quality and group tail cut off. Quality, source and group are read
// by Parse as for any release.
type Scene struct {
	Studio string `json:"studio"`
	// Date is YYYY-MM-DD.
	Date string `json:"date"`
	// Rest is the performers and title in the order of the name.
	Rest string `json:"rest"`
}

var (
	// Studio.24.01.15.Rest… or Studio.2024.01.15.Rest…
	sceneDotted = regexp.MustCompile(`^(?P<studio>.+?)[ ._-]+(?P<y>(?:19|20)?\d{2})[ ._-](?P<m>\d{2})[ ._-](?P<d>\d{2})(?:[ ._-]+(?P<rest>.*))?$`)
	// [Studio] Rest (2024-01-15) or Studio - Rest - 2024-01-15
	sceneTrailing = regexp.MustCompile(`^(?:\[(?P<studio>[^\]]+)\]|(?P<studio2>[^-]+?)\s+-)\s*(?P<rest>.+?)\s*[\[(]?(?P<y>(?:19|20)\d{2})-(?P<m>\d{2})-(?P<d>\d{2})[\])]?(?:[ ._-]+.*)?$`)
	// [Studio] Rest (15.01.2024) or Studio - Rest [15.01.2024]: a day-first
	// date in brackets, as uploaders of general sites write it.
	sceneTrailingDMY = regexp.MustCompile(`^(?:\[(?P<studio>[^\]]+)\]|(?P<studio2>[^-\[(]+?)\s+-)\s*(?P<rest>.+?)\s*[\[(](?P<d>\d{2})\.(?P<m>\d{2})\.(?P<y>(?:19|20)\d{2})[\])](?:[ ._-]+.*)?$`)
	sceneTail        = regexp.MustCompile(`(?i)^(?:xxx|\d{3,4}p|4k|uhd|sd|fhd|fullhd|mp4|wmv|mkv|avi|web(?:-?dl|-?rip)?|hdtv|x26[45]|h\.?26[45]|hevc|avc|aac|prt|ktr|imagesets?|image ?sets?|imgsets?|pics|pictures|photos?|photosets?|images|jpe?g|png|webp|zip|rar|x\d{2,5}|\d{2,5}x|split[_ ]?scenes)$`)
)

// ParseScene reads a scene name such as
// "Studio.Name.24.01.15.Performer.One.Scene.Title.1080p.MP4-GRP". It reports
// false when the name has no studio and date.
func ParseScene(name string) (Scene, bool) {
	name = strings.TrimSpace(stripExtension(name))
	if name == "" {
		return Scene{}, false
	}
	var studio, y, m, d, rest string
	if g := sceneTrailing.FindStringSubmatch(name); g != nil {
		studio = g[sceneTrailing.SubexpIndex("studio")] + g[sceneTrailing.SubexpIndex("studio2")]
		y, m, d = g[sceneTrailing.SubexpIndex("y")], g[sceneTrailing.SubexpIndex("m")], g[sceneTrailing.SubexpIndex("d")]
		rest = g[sceneTrailing.SubexpIndex("rest")]
	} else if g := sceneTrailingDMY.FindStringSubmatch(name); g != nil {
		studio = g[sceneTrailingDMY.SubexpIndex("studio")] + g[sceneTrailingDMY.SubexpIndex("studio2")]
		y, m, d = g[sceneTrailingDMY.SubexpIndex("y")], g[sceneTrailingDMY.SubexpIndex("m")], g[sceneTrailingDMY.SubexpIndex("d")]
		rest = g[sceneTrailingDMY.SubexpIndex("rest")]
	} else if g := sceneDotted.FindStringSubmatch(name); g != nil {
		studio = g[sceneDotted.SubexpIndex("studio")]
		y, m, d = g[sceneDotted.SubexpIndex("y")], g[sceneDotted.SubexpIndex("m")], g[sceneDotted.SubexpIndex("d")]
		rest = g[sceneDotted.SubexpIndex("rest")]
	} else {
		return Scene{}, false
	}
	date, ok := sceneDate(y, m, d)
	if !ok {
		return Scene{}, false
	}
	studio = sceneWords(studio, false)
	if studio == "" {
		return Scene{}, false
	}
	return Scene{Studio: studio, Date: date, Rest: sceneWords(rest, true)}, true
}

func stripExtension(name string) string {
	if i := strings.LastIndexByte(name, '.'); i > 0 && len(name)-i <= 5 {
		switch strings.ToLower(name[i+1:]) {
		case "mkv", "mp4", "avi", "wmv", "m4v", "mov", "ts", "nzb", "torrent":
			return name[:i]
		}
	}
	return name
}

func sceneDate(y, m, d string) (string, bool) {
	yy, _ := strconv.Atoi(y)
	if len(y) == 2 {
		yy += 2000
	}
	mm, _ := strconv.Atoi(m)
	dd, _ := strconv.Atoi(d)
	if yy < 1970 || yy > 2100 || mm < 1 || mm > 12 || dd < 1 || dd > 31 {
		return "", false
	}
	return fmt.Sprintf("%04d-%02d-%02d", yy, mm, dd), true
}

// sceneWords turns separators into spaces; with tail it stops at the first
// quality or format word and drops a trailing "-GROUP".
func sceneWords(s string, tail bool) string {
	s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	if tail {
		if i := strings.LastIndex(s, "-"); i > 0 && !strings.Contains(s[i+1:], " ") {
			s = s[:i]
		}
	}
	var out []string
	for _, w := range strings.Fields(s) {
		w = strings.Trim(w, "-[]()")
		if w == "" {
			continue
		}
		if sceneTail.MatchString(w) {
			if tail {
				break
			}
			continue
		}
		out = append(out, w)
	}
	return strings.Join(out, " ")
}

