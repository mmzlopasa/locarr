package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/api"
	"github.com/mmzlopasa/locarr/server/internal/config"
	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver/streamero"
	"github.com/mmzlopasa/locarr/server/internal/metadata/musicbrainz"
	"github.com/mmzlopasa/locarr/server/internal/notify"
	"github.com/mmzlopasa/locarr/server/internal/notify/delivery"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/subtitles"
)

// buildMediaServer creates the Streamero integration and the notification
// channels (P9). It runs before buildRequests, which wraps the notifiers
// again; the hub is read when an event happens.
func buildMediaServer(_ context.Context, d *Deps, cfg config.Config, log *slog.Logger) {
	d.Streamero = &streamero.Service{DB: d.DB, Settings: d.Settings, Roots: d.Roots, Timeline: d.Timeline, Jobs: d.Jobs,
		Policy: d.Indexers.Policy, Log: log}
	d.Channels = &delivery.Service{DB: d.DB, Box: d.Box, Settings: d.Settings, Jobs: d.Jobs, Log: log, Policy: d.Indexers.Policy,
		TelegramAPI: cfg.TelegramURL, PushoverAPI: cfg.PushoverURL}
	d.Channels.Register()
	d.Auth.RegisterProvider(streamero.Provider{S: d.Streamero})

	d.Jobs.Register(streamero.TaskScan, d.Streamero.RunScan, jobs.KindOptions{MaxAttempts: 6, Timeout: 5 * time.Minute})
	d.Jobs.Register(streamero.TaskIndex, func(ctx context.Context, _ *jobs.Job) error {
		n, err := d.Streamero.Refresh(ctx)
		if err == nil && n > 0 {
			log.Info("Streamero titles indexed", "count", n)
		}
		return err
	}, jobs.KindOptions{MaxAttempts: 2, Timeout: 30 * time.Minute})
	d.Jobs.Schedule(streamero.TaskIndex, streamero.TaskIndex, streamero.IndexInterval)

	hub := func() *notify.Hub { return d.Hub }
	d.Movies.Notifier = libraryEvents{next: streamero.Notifier{S: d.Streamero}, hub: hub, d: d}
	d.Series.Notifier = libraryEvents{next: streamero.Notifier{S: d.Streamero}, hub: hub, d: d}
	d.Music.Notifier = libraryEvents{next: streamero.Notifier{S: d.Streamero}, hub: hub, d: d}

	d.Downloads.Bus.Subscribe(func(ctx context.Context, ev downloads.Event) {
		var typ notify.Type
		msg := ev.Download.Title
		switch ev.Type {
		case downloads.EventGrabbed:
			typ = notify.LibraryGrabbed
		case downloads.EventFailed:
			typ = notify.LibraryDownloadFailed
			msg = joinReason(ev.Download.Title, ev.Message, ev.Reason)
		case downloads.EventImportFailed:
			typ = notify.LibraryImportFailed
			msg = joinReason(ev.Download.Title, ev.Message, ev.Reason)
		default:
			return
		}
		ref := library.RefFromJSON(ev.Download.ItemRef)
		if library.IsAdult(ref.Kind) {
			// Adult downloads never reach the library events (D-103); only
			// channels that chose adult events hear about them.
			if d.Adult != nil {
				d.Adult.Events(ctx, "adult."+strings.TrimPrefix(string(typ), "library."), ev.Download.Title, msg, ref.String())
			}
			return
		}
		info := describeRef(ctx, d, ref)
		if info.title == "" {
			info.title = ev.Download.Title
		}
		if h := hub(); h != nil {
			h.Publish(ctx, notify.Event{Type: typ, Title: info.title, Message: msg, Poster: info.poster, MediaKind: info.kind,
				ItemRef: info.ref, Params: map[string]any{"downloadId": ev.Download.ID, "indexer": ev.Download.Indexer}})
		}
	})
}

func joinReason(title, msg, reason string) string {
	if msg == "" {
		msg = reason
	}
	if msg == "" {
		return title
	}
	return title + ": " + msg
}

// attachMediaServer gives the handler Streamero and the channels.
func attachMediaServer(h *api.Handler, d *Deps) {
	h.Streamero, h.Channels = d.Streamero, d.Channels
}

// libraryEvents publishes imports and upgrades to the channels and passes
// every change on to the media server notifier.
type libraryEvents struct {
	next mediaserver.Notifier
	hub  func() *notify.Hub
	d    *Deps
}

// Name is never empty, so the library always reports changes here.
func (l libraryEvents) Name() string {
	if n := l.next.Name(); n != "" {
		return n
	}
	return "channels"
}

// RecordsOutcome: with no media server there is nothing to record.
func (l libraryEvents) RecordsOutcome() bool {
	return l.next.Name() == "" || mediaserver.RecordsOutcome(l.next)
}

func (l libraryEvents) Notify(ctx context.Context, c mediaserver.Change) error {
	if c.Action == "imported" || c.Action == "upgraded" {
		typ := notify.LibraryImported
		if c.Action == "upgraded" {
			typ = notify.LibraryUpgraded
		}
		ref, _ := library.ParseRef(c.ItemRef)
		info := describeRef(ctx, l.d, ref)
		if info.title == "" {
			info.title = c.Title
		}
		if h := l.hub(); h != nil {
			h.Publish(ctx, notify.Event{Type: typ, Title: info.title, Poster: info.poster, MediaKind: info.kind, ItemRef: info.ref})
		}
	}
	if l.next.Name() == "" {
		return nil
	}
	return l.next.Notify(ctx, c)
}

type refInfo struct {
	title, poster, kind, ref string
}

// describeRef finds the title, poster and page of a library item.
func describeRef(ctx context.Context, d *Deps, r library.Ref) refInfo {
	var (
		info  refInfo
		title string
		year  int
		id    = r.ID
		err   error
	)
	switch r.Kind {
	case library.RefEpisode:
		err = d.DB.R.QueryRowContext(ctx, `SELECT series_id FROM episodes WHERE id = ?`, r.ID).Scan(&id)
		r.Kind = library.RefSeries
	case library.RefSeason:
		err = d.DB.R.QueryRowContext(ctx, `SELECT series_id FROM seasons WHERE id = ?`, r.ID).Scan(&id)
		r.Kind = library.RefSeries
	}
	if err != nil {
		return info
	}
	switch r.Kind {
	case library.RefMovie:
		err = d.DB.R.QueryRowContext(ctx, `SELECT i.title, i.year, m.poster FROM media_items i JOIN movies m ON m.item_id = i.id WHERE i.id = ?`,
			id).Scan(&title, &year, &info.poster)
		info.kind = "movie"
	case library.RefSeries:
		err = d.DB.R.QueryRowContext(ctx, `SELECT i.title, i.year, s.poster FROM media_items i JOIN series s ON s.item_id = i.id WHERE i.id = ?`,
			id).Scan(&title, &year, &info.poster)
		info.kind = "series"
	case library.RefAlbum:
		var artist, mbid string
		err = d.DB.R.QueryRowContext(ctx, `SELECT a.title, i.title, a.mbid FROM albums a JOIN media_items i ON i.id = a.artist_id WHERE a.id = ?`,
			id).Scan(&title, &artist, &mbid)
		title, info.poster, info.kind = artist+" – "+title, musicbrainz.CoverName(mbid), "album"
	case library.RefArtist:
		err = d.DB.R.QueryRowContext(ctx, `SELECT title FROM media_items WHERE id = ?`, id).Scan(&title)
		info.kind = "artist"
	default:
		return info
	}
	if errors.Is(err, sql.ErrNoRows) || err != nil {
		return refInfo{}
	}
	info.title = title
	if year > 0 {
		info.title = fmt.Sprintf("%s (%d)", title, year)
	}
	info.ref = r.Kind + ":" + strconv.FormatInt(id, 10)
	return info
}

// wireSubtitleEvents sends subtitle downloads to the channels, and a file's
// still-missing subtitles once, when it arrived more than
// notifications.subtitleMissingDays ago.
func wireSubtitleEvents(d *Deps, log *slog.Logger) {
	d.Subtitles.Notify = func(ctx context.Context, ref, typ, msg string) {
		r, err := library.ParseRef(ref)
		// Subtitle events (downloaded, translated, missing) never name adult
		// items; the adult library has no subtitle features (D-103).
		if err != nil || d.Hub == nil || library.IsAdult(r.Kind) {
			return
		}
		switch typ {
		case subtitles.TypeDownloaded, subtitles.TypeUpgraded:
			info := describeRef(ctx, d, r)
			d.Hub.Publish(ctx, notify.Event{Type: notify.SubtitleDownloaded, Title: info.title, Message: msg, Poster: info.poster,
				MediaKind: info.kind, ItemRef: info.ref})
		case subtitles.TypeTranslated:
			info := describeRef(ctx, d, r)
			d.Hub.Publish(ctx, notify.Event{Type: notify.SubtitleTranslated, Title: info.title, Message: msg, Poster: info.poster,
				MediaKind: info.kind, ItemRef: info.ref})
		case subtitles.TypeNotFound:
			days, err := settings.Get(ctx, d.Settings, delivery.SubtitleMissingDays)
			if err != nil || days <= 0 {
				return
			}
			var (
				fileID int64
				added  int64
				miss   string
			)
			if err := d.DB.R.QueryRowContext(ctx, `SELECT media_file_id, file_added_at, missing FROM subtitle_state WHERE item_ref = ?`, ref).
				Scan(&fileID, &added, &miss); err != nil {
				return
			}
			if time.Since(db.Time(added)) < time.Duration(days)*24*time.Hour {
				return
			}
			res, err := d.DB.W.ExecContext(ctx, `INSERT INTO subtitle_missing_notified (media_file_id, notified_at) VALUES (?, ?)
				ON CONFLICT (media_file_id) DO NOTHING`, fileID, db.Millis(time.Now()))
			if err != nil {
				log.Warn("remember subtitle notification", "item", ref, "err", err)
				return
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return
			}
			var keys []string
			_ = json.Unmarshal([]byte(miss), &keys)
			info := describeRef(ctx, d, r)
			d.Hub.Publish(ctx, notify.Event{Type: notify.SubtitleMissing, Title: info.title, Message: strings.Join(keys, ", "),
				Poster: info.poster, MediaKind: info.kind, ItemRef: info.ref, Params: map[string]any{"days": days, "languages": keys}})
		}
	}
}
