-- +goose Up
-- D-109. Adult items get a quality profile that allows every video quality,
-- Unknown included: general sites often name scenes without a resolution or
-- source, and every other profile refuses Unknown. The profile has a neutral
-- name (nothing adult shows in the shared profile list, D-103). It becomes
-- the default for new adult items unless the administrator chose one;
-- existing items keep their profile (Settings -> Adult content offers to
-- switch them).
INSERT INTO quality_profiles (name, kind, data, created_at, updated_at)
SELECT 'Any (Unknown allowed)', 'movies',
    '{"id":0,"name":"Any (Unknown allowed)","kind":"movies","items":[{"quality":"webdl-4320p","allowed":true},{"quality":"brdisk","allowed":true},{"quality":"remux-2160p","allowed":true},{"quality":"bluray-2160p","allowed":true},{"quality":"webrip-2160p","allowed":true},{"quality":"webdl-2160p","allowed":true},{"quality":"hdtv-2160p","allowed":true},{"quality":"rawhd","allowed":true},{"quality":"remux-1080p","allowed":true},{"quality":"bluray-1080p","allowed":true},{"quality":"webrip-1080p","allowed":true},{"quality":"webdl-1080p","allowed":true},{"quality":"hdtv-1080p","allowed":true},{"quality":"bluray-720p","allowed":true},{"quality":"webrip-720p","allowed":true},{"quality":"webdl-720p","allowed":true},{"quality":"hdtv-720p","allowed":true},{"quality":"bluray-576p","allowed":true},{"quality":"bluray-480p","allowed":true},{"quality":"webrip-480p","allowed":true},{"quality":"webdl-480p","allowed":true},{"quality":"dvdr","allowed":true},{"quality":"dvd","allowed":true},{"quality":"sdtv","allowed":true},{"quality":"screener","allowed":true},{"quality":"regional","allowed":true},{"quality":"telecine","allowed":true},{"quality":"telesync","allowed":true},{"quality":"cam","allowed":true},{"quality":"workprint","allowed":true},{"quality":"unknown","allowed":true}],"cutoff":"webdl-1080p","upgradesAllowed":true,"minFormatScore":0,"cutoffFormatScore":0,"formatScores":[],"languages":[]}',
    CAST(strftime('%s', 'now') AS INTEGER) * 1000, CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE NOT EXISTS (SELECT 1 FROM quality_profiles WHERE kind = 'movies' AND name = 'Any (Unknown allowed)');

INSERT OR REPLACE INTO settings (key, value, updated_at)
SELECT 'adult.openProfileId', CAST(id AS TEXT), CAST(strftime('%s', 'now') AS INTEGER) * 1000
FROM quality_profiles WHERE kind = 'movies' AND name = 'Any (Unknown allowed)';

INSERT OR REPLACE INTO settings (key, value, updated_at)
SELECT 'adult.defaultProfileId', CAST(id AS TEXT), CAST(strftime('%s', 'now') AS INTEGER) * 1000
FROM quality_profiles WHERE kind = 'movies' AND name = 'Any (Unknown allowed)'
  AND COALESCE((SELECT CAST(value AS INTEGER) FROM settings WHERE key = 'adult.defaultProfileId'), 0) = 0;

-- The tag blocklist starts empty and holds only the owner's own terms: the
-- seeded suggestions go and are never seeded again. A fixed set of terms for
-- content involving minors is always applied in code;
-- an owner's copy of one of those terms is redundant and goes too.
DELETE FROM adult_tag_blocklist WHERE builtin = 1 OR term IN (
    'underage', 'under age', 'child', 'children', 'preteen', 'pre teen', 'jailbait', 'lolita', 'loli', 'shota', 'pthc'
);

-- +goose Down
DELETE FROM settings WHERE key = 'adult.openProfileId';
