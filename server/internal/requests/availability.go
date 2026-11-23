package requests

// SeasonFacts are the library's numbers for one season of a series.
type SeasonFacts struct {
	Number int
	// Aired counts episodes that have aired (not removed); Files those of
	// them with a file.
	Aired int
	Files int
	// Downloading counts episodes with an active download.
	Downloading int
}

// SeasonStatus is a season's status from its facts: available when every
// aired episode has a file, partial when some have, else processing (it may
// not have aired yet).
func SeasonStatus(f SeasonFacts) Status {
	switch {
	case f.Aired > 0 && f.Files >= f.Aired:
		return StatusAvailable
	case f.Files > 0:
		return StatusPartial
	}
	return StatusProcessing
}

// SeriesStatus is the status of a series request for seasons from the facts
// by season number, and each season's own status. A season the library does
// not know (yet) is processing.
func SeriesStatus(seasons []int, facts map[int]SeasonFacts) (Status, map[int]Status) {
	per := make(map[int]Status, len(seasons))
	avail, some := 0, false
	for _, n := range seasons {
		st := StatusProcessing
		if f, ok := facts[n]; ok {
			st = SeasonStatus(f)
		}
		per[n] = st
		switch st {
		case StatusAvailable:
			avail++
			some = true
		case StatusPartial:
			some = true
		}
	}
	switch {
	case len(seasons) > 0 && avail == len(seasons):
		return StatusAvailable, per
	case some:
		return StatusPartial, per
	}
	return StatusProcessing, per
}

// TrackStatus is an album's (or an artist's) status from its file and track
// counts.
func TrackStatus(files, tracks int) Status {
	switch {
	case tracks > 0 && files >= tracks:
		return StatusAvailable
	case files > 0:
		return StatusPartial
	}
	return StatusProcessing
}

// MovieStatus is a movie's status.
func MovieStatus(hasFile bool) Status {
	if hasFile {
		return StatusAvailable
	}
	return StatusProcessing
}

// failCodes are explainer codes that mean the library is stuck: the request
// shows as failed (with the reason) until something changes (D-054).
var failCodes = map[string]bool{"nothing_found": true, "all_rejected": true, "import_failed": true, "download_failed": true,
	"no_indexers": true, "no_download_client": true, "not_monitored": true}

// Stuck reports an explainer code that makes an unfinished request failed.
func Stuck(code string) bool { return failCodes[code] }
