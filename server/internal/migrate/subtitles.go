package migrate

import (
	"context"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/subtitles"
)

// SubtitlesAdapter imports Bazarr settings through the subtitle service.
type SubtitlesAdapter struct{ S *subtitles.Service }

var _ SubtitleTarget = SubtitlesAdapter{}

// Profiles returns the subtitle profiles by lower-cased name.
func (a SubtitlesAdapter) Profiles(ctx context.Context) (map[string]int64, error) {
	list, err := a.S.Profiles(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, p := range list {
		out[strings.ToLower(p.Name)] = p.ID
	}
	return out, nil
}

// SubtitleProfileInput maps a Bazarr language profile: a language marked
// "hearing impaired" wants only HI subtitles, the others accept them; the
// cutoff stops searching once the first language is found.
func SubtitleProfileInput(p SubtitleProfile) subtitles.ProfileInput {
	in := subtitles.ProfileInput{Name: p.Name, Cutoff: p.Cutoff}
	seen := map[string]bool{}
	for _, l := range p.Languages {
		code := MapLanguage(l.Code)
		if code == "" || code == "any" || code == "original" {
			continue
		}
		w := subtitles.Want{Language: code, Forced: l.Forced, HearingImpaired: subtitles.HIInclude}
		if l.HI {
			w.HearingImpaired = subtitles.HIOnly
		}
		if seen[w.Key()] {
			continue
		}
		seen[w.Key()] = true
		in.Languages = append(in.Languages, w)
	}
	return in
}

// SaveProfile creates or replaces a profile.
func (a SubtitlesAdapter) SaveProfile(ctx context.Context, id int64, p SubtitleProfile) (int64, error) {
	in := SubtitleProfileInput(p)
	if id > 0 {
		saved, err := a.S.UpdateProfile(ctx, id, in)
		return saved.ID, err
	}
	saved, err := a.S.CreateProfile(ctx, in)
	return saved.ID, err
}

// bazarrProviders maps Bazarr provider names to Locarr's.
var bazarrProviders = map[string]string{"opensubtitlescom": "opensubtitles", "subdl": "subdl", "jimaku": "jimaku"}

// Provider reports whether Locarr has the provider.
func (a SubtitlesAdapter) Provider(name string) (string, bool) {
	n, ok := bazarrProviders[strings.ToLower(name)]
	if !ok {
		return "", false
	}
	_, ok = subtitles.ImplementationOf(n)
	return n, ok
}

// EnableProvider turns a provider on with the credentials found.
func (a SubtitlesAdapter) EnableProvider(ctx context.Context, name string, fields map[string]string) error {
	info, _ := subtitles.ImplementationOf(name)
	on := !info.NeedsKey || fields["apikey"] != ""
	in := subtitles.ProviderInput{Enabled: &on}
	if v, ok := fields["username"]; ok {
		in.Username = &v
	}
	if v, ok := fields["password"]; ok {
		in.Password = &v
	}
	if v, ok := fields["apikey"]; ok {
		in.APIKey = &v
	}
	_, err := a.S.UpdateProvider(ctx, name, in)
	return err
}

// Assign gives library items a profile.
func (a SubtitlesAdapter) Assign(ctx context.Context, itemIDs []int64, profileID int64) (int, error) {
	return a.S.AssignProfile(ctx, itemIDs, profileID)
}
