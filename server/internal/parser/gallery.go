package parser

import (
	"regexp"
	"strconv"
	"strings"
)

// Gallery is what a release name says about an image set (D-113): whether
// it calls itself one, how many images it has and how large they are, and
// whether it rather looks like a video.
type Gallery struct {
	// Images: the name says image set, pictures, photos, …
	Images bool `json:"images"`
	// Video: the name has video markers (a resolution such as 1080p, a video
	// container or codec).
	Video bool `json:"video"`
	// Count is the number of images the name gives ("x150", "150 pics"), 0
	// when it says none.
	Count int `json:"count,omitempty"`
	// Pixels is the long side of the images when the name gives it
	// ("4000px", "3000x4500"), 0 otherwise.
	Pixels int `json:"pixels,omitempty"`
}

var (
	galleryWords = regexp.MustCompile(`(?i)(?:^|[^a-z])(?:image ?sets?|imgsets?|pic ?sets?|photo ?sets?|pics|pictures|photos|images|gallery|galleries|jpe?gs?)(?:[^a-z]|$)`)
	// x150, 150x (a count, never a codec or a WxH size)
	galleryCount   = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:x(\d{2,5})|(\d{2,5})x)(?:[^a-z0-9]|$)`)
	galleryCountOf = regexp.MustCompile(`(?i)(?:^|[^0-9])(\d{1,5})[ ._-]?(?:pics|pictures|photos|images|imgs|jpe?gs?)(?:[^a-z]|$)`)
	galleryPx      = regexp.MustCompile(`(?i)(?:^|[^0-9])(\d{3,5})[ ._-]?px(?:[^a-z]|$)`)
	galleryWxH     = regexp.MustCompile(`(?:^|[^0-9])(\d{3,5})x(\d{3,5})(?:[^0-9]|$)`)
	videoMarkers   = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:(?:360|480|540|576|720|1080|1440|2160|4320)p|4k|uhd|fullhd|fhd|mp4|wmv|mkv|avi|m4v|mov|web-?dl|webrip|hdtv|x26[45]|h\.?26[45]|hevc|avc|xvid|divx)(?:[^a-z0-9]|$)`)
)

// ParseGallery reads the image set hints of a release name.
func ParseGallery(name string) Gallery {
	var g Gallery
	g.Images = galleryWords.MatchString(name)
	for _, m := range galleryCount.FindAllStringSubmatch(name, -1) {
		v := m[1] + m[2]
		if v == "264" || v == "265" {
			continue
		}
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			g.Count = n
			g.Images = true
			break
		}
	}
	if m := galleryCountOf.FindStringSubmatch(name); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			g.Count = n
		}
	}
	if m := galleryPx.FindStringSubmatch(name); m != nil {
		g.Pixels, _ = strconv.Atoi(m[1])
	}
	if m := galleryWxH.FindStringSubmatch(name); m != nil && g.Images {
		w, _ := strconv.Atoi(m[1])
		h, _ := strconv.Atoi(m[2])
		g.Pixels = max(w, h)
	}
	// "1920x1080" alone is a video size; with image words it is the images'.
	g.Video = videoMarkers.MatchString(strings.NewReplacer("_", " ").Replace(name))
	return g
}
