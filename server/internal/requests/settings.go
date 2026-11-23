package requests

import (
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

// KindDefaults are where approved requests of one kind go (0 = not chosen).
type KindDefaults struct {
	ProfileID           int64                `json:"profileId"`
	RootFolderID        int64                `json:"rootFolderId"`
	Profile4KID         int64                `json:"profile4kId"`
	RootFolder4KID      int64                `json:"rootFolder4kId"`
	MetadataProfileID   int64                `json:"metadataProfileId"`
	MinimumAvailability library.Availability `json:"minimumAvailability"`
	Tags                []string             `json:"tags"`
}

// Settings are the request defaults and the default quotas.
type Settings struct {
	Movie  KindDefaults    `json:"movie"`
	Series KindDefaults    `json:"series"`
	Music  KindDefaults    `json:"music"`
	Quotas map[string]Rule `json:"quotas"`
}

// DefaultQuota is the quota until an administrator sets one: unlimited.
var DefaultQuota = Rule{Limit: 0, Days: 7}

// SettingsKey stores Settings.
var SettingsKey = settings.Key[Settings]{Name: "requests.settings", Default: Settings{}}

// For returns the defaults of a kind.
func (s Settings) For(k Kind) KindDefaults {
	switch k {
	case KindMovie:
		return s.Movie
	case KindSeries:
		return s.Series
	}
	return s.Music
}

// Quota returns the default rule of a quota kind.
func (s Settings) Quota(kind string) Rule {
	if r, ok := s.Quotas[kind]; ok {
		if r.Days <= 0 {
			r.Days = DefaultQuota.Days
		}
		return r
	}
	return DefaultQuota
}

// Normalise fills missing quota kinds and tags.
func (s *Settings) Normalise() {
	if s.Quotas == nil {
		s.Quotas = map[string]Rule{}
	}
	for _, k := range QuotaKinds {
		s.Quotas[k] = s.Quota(k)
	}
	for _, d := range []*KindDefaults{&s.Movie, &s.Series, &s.Music} {
		if d.Tags == nil {
			d.Tags = []string{}
		}
	}
}
