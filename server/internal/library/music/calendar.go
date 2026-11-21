package music

import "context"

// CalendarEntry is an album release on a day.
type CalendarEntry struct {
	Date       string
	AlbumID    int64
	ArtistID   int64
	ArtistName string
	Title      string
	Type       string
	Monitored  bool
	HasFiles   bool
	Cover      string // release group id
}

// Calendar lists album releases between two days (YYYY-MM-DD, inclusive).
func (s *Service) Calendar(ctx context.Context, start, end string) ([]CalendarEntry, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT al.release_date, al.id, al.artist_id, i.title, al.title, al.primary_type, al.monitored AND i.monitored,
		EXISTS (SELECT 1 FROM album_files af WHERE af.album_id = al.id), al.mbid
		FROM albums al JOIN media_items i ON i.id = al.artist_id
		WHERE length(al.release_date) = 10 AND al.release_date BETWEEN ? AND ? ORDER BY al.release_date, i.sort_title`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CalendarEntry{}
	for rows.Next() {
		var e CalendarEntry
		if err := rows.Scan(&e.Date, &e.AlbumID, &e.ArtistID, &e.ArtistName, &e.Title, &e.Type, &e.Monitored, &e.HasFiles, &e.Cover); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
