// Package importer is the shared import machinery: find the media files in a
// finished download or a folder, tell main files from samples and extras,
// carry subtitle sidecars along, and transfer files into the library with the
// configured mode. Each media kind decides what a file is (matching) and where
// it goes (naming); see the movies package for the movie importer.
package importer

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Reasons a file is not imported.
const (
	SkipSample     = "sample"
	SkipExtra      = "extra"
	SkipSmall      = "smaller_than_main"
	SkipDisc       = "disc_structure"
	SkipNotMedia   = "not_media"
	SkipHidden     = "hidden"
	SkipUnreadable = "unreadable"
)

// VideoExtensions are imported as movie or episode files.
var VideoExtensions = map[string]bool{".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".mov": true, ".wmv": true,
	".ts": true, ".m2ts": true, ".mpg": true, ".mpeg": true, ".webm": true, ".flv": true, ".divx": true, ".xvid": true, ".ogm": true}

// extraFolders hold bonus material, never the main file.
var extraFolders = map[string]bool{"sample": true, "samples": true, "extras": true, "extra": true, "featurettes": true,
	"behind the scenes": true, "deleted scenes": true, "interviews": true, "trailers": true, "shorts": true, "scenes": true,
	"other": true, "bonus": true, "specials": false, "subs": false}

var sampleName = regexp.MustCompile(`(?i)(^|[.\-_ \[(])sample([.\-_ \])]|$)`)

// File is one file found.
type File struct {
	Path string
	Rel  string // relative to the scanned folder
	Size int64
	// Skip is why it is not imported ("" = import it).
	Skip string
}

// Discovery is what a folder holds.
type Discovery struct {
	Root string
	// Videos are the importable video files, largest first.
	Videos []File
	// Sidecars are subtitle and other extra files with a wanted extension.
	Sidecars []File
	// Skipped lists the video files left out, with the reason.
	Skipped []File
	// Disc is set when the folder is a Blu-ray/DVD structure or an image.
	Disc bool
}

// maxDepth limits how deep a download folder is searched.
const maxDepth = 4

// minMainShare: another video smaller than this share of the largest is an extra.
const minMainShare = 0.3

// Discover lists the media files at root (a file or a folder). extras are the
// sidecar extensions without dots ("srt", "ass", …).
func Discover(root string, extras []string) (Discovery, error) {
	d := Discovery{Root: root}
	extraSet := map[string]bool{}
	for _, e := range extras {
		extraSet["."+strings.TrimPrefix(strings.ToLower(e), ".")] = true
	}
	st, err := os.Stat(root)
	if err != nil {
		return d, err
	}
	if !st.IsDir() {
		f := File{Path: root, Rel: filepath.Base(root), Size: st.Size()}
		ext := strings.ToLower(filepath.Ext(root))
		switch {
		case ext == ".iso" || ext == ".img":
			d.Disc = true
			f.Skip = SkipDisc
			d.Skipped = append(d.Skipped, f)
		case VideoExtensions[ext]:
			d.Videos = append(d.Videos, f)
		default:
			f.Skip = SkipNotMedia
			d.Skipped = append(d.Skipped, f)
		}
		return d, nil
	}
	err = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil // unreadable parts are skipped
		}
		rel, _ := filepath.Rel(root, p)
		name := e.Name()
		if e.IsDir() {
			if p == root {
				return nil
			}
			lower := strings.ToLower(name)
			if lower == "bdmv" || lower == "video_ts" || lower == "hvdvd_ts" {
				d.Disc = true
				return filepath.SkipDir
			}
			if strings.HasPrefix(name, ".") || strings.Count(rel, string(filepath.Separator)) >= maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if !e.Type().IsRegular() || strings.HasPrefix(name, ".") {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return nil
		}
		f := File{Path: p, Rel: rel, Size: info.Size()}
		ext := strings.ToLower(filepath.Ext(name))
		switch {
		case ext == ".iso" || ext == ".img":
			d.Disc = true
			f.Skip = SkipDisc
			d.Skipped = append(d.Skipped, f)
		case VideoExtensions[ext]:
			switch folder := extraFolder(rel); {
			case folder == "sample" || folder == "samples" || sampleName.MatchString(strings.TrimSuffix(name, filepath.Ext(name))):
				f.Skip = SkipSample
			case folder != "":
				f.Skip = SkipExtra
			}
			if f.Skip != "" {
				d.Skipped = append(d.Skipped, f)
			} else {
				d.Videos = append(d.Videos, f)
			}
		case extraSet[ext]:
			d.Sidecars = append(d.Sidecars, f)
		}
		return nil
	})
	if err != nil {
		return d, err
	}
	sort.SliceStable(d.Videos, func(i, j int) bool { return d.Videos[i].Size > d.Videos[j].Size })
	return d, nil
}

// extraFolder returns the lower-case name of the bonus-material folder rel
// lies in ("" = none).
func extraFolder(rel string) string {
	for _, p := range strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/") {
		if extraFolders[strings.ToLower(p)] {
			return strings.ToLower(p)
		}
	}
	return ""
}

// MainVideo returns the single main video of a movie-like download: the
// largest, with every much smaller video moved to Skipped. ok is false when
// there is none.
func (d *Discovery) MainVideo() (File, bool) {
	if len(d.Videos) == 0 {
		return File{}, false
	}
	main := d.Videos[0]
	keep := d.Videos[:1]
	for _, v := range d.Videos[1:] {
		if float64(v.Size) < float64(main.Size)*minMainShare {
			v.Skip = SkipSmall
			d.Skipped = append(d.Skipped, v)
			continue
		}
		keep = append(keep, v)
	}
	d.Videos = keep
	return main, true
}

// SidecarSuffix returns what follows the video's base name in a sidecar's
// name, e.g. ".en.forced.srt" for "Movie.en.forced.srt" next to "Movie.mkv",
// or the language part and extension of a sidecar with another name
// ("Subs/2_English.srt" -> ".srt" plus nothing, "English.en.srt" -> ".en.srt").
func SidecarSuffix(videoPath, sidecarPath string) string {
	vbase := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	sname := filepath.Base(sidecarPath)
	if strings.HasPrefix(strings.ToLower(sname), strings.ToLower(vbase)+".") {
		return strings.ToLower(sname[len(vbase):])
	}
	ext := strings.ToLower(filepath.Ext(sname))
	rest := strings.TrimSuffix(sname, filepath.Ext(sname))
	parts := strings.Split(rest, ".")
	var keep []string
	for i := len(parts) - 1; i >= 1 && len(keep) < 3; i-- {
		p := strings.ToLower(parts[i])
		if len(p) == 2 || len(p) == 3 || p == "forced" || p == "sdh" || p == "cc" || p == "hi" {
			keep = append([]string{p}, keep...)
			continue
		}
		break
	}
	if len(keep) == 0 {
		return ext
	}
	return "." + strings.Join(keep, ".") + ext
}
