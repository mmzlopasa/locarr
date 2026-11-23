package settings

// System, health, backup and housekeeping settings (P11, D-083 … D-086).
var (
	// UpdateCheck asks GitHub once a day whether a newer Locarr exists.
	UpdateCheck = Key[bool]{Name: "system.updateCheck", Default: true}
	// MinFreeSpaceGB: root folders and download clients with less free space are reported.
	MinFreeSpaceGB = Key[int]{Name: "health.minFreeSpaceGb", Default: 10}
	// RecycleBinWarnGB: a larger recycle bin is reported (0 = never).
	RecycleBinWarnGB = Key[int]{Name: "health.recycleBinWarnGb", Default: 50}
	// StuckImportMinutes: a finished download not imported for this long is reported.
	StuckImportMinutes = Key[int]{Name: "health.stuckImportMinutes", Default: 60}
	// BackupIntervalHours between scheduled backups (0 = off) and how many are kept.
	BackupIntervalHours = Key[int]{Name: "backup.intervalHours", Default: 24}
	BackupKeep          = Key[int]{Name: "backup.keep", Default: 7}
	// Retention of history-like tables, in days.
	TimelineDays       = Key[int]{Name: "housekeeping.timelineDays", Default: 365}
	SearchLogDays      = Key[int]{Name: "housekeeping.searchLogDays", Default: 90}
	DownloadEventsDays = Key[int]{Name: "housekeeping.downloadEventsDays", Default: 180}
	// VacuumIntervalDays between database compactions (0 = never).
	VacuumIntervalDays = Key[int]{Name: "housekeeping.vacuumIntervalDays", Default: 7}
	// ImageCacheMaxMB caps the poster and cover cache.
	ImageCacheMaxMB = Key[int]{Name: "housekeeping.imageCacheMaxMb", Default: 1024}
	// LastVacuum is when the database was last compacted (Unix milliseconds).
	LastVacuum = Key[int64]{Name: "housekeeping.lastVacuum", Default: 0}
	// MetricsToken turns /metrics on (bearer token; unset = off).
	MetricsToken = SecretKey{Name: "system.metricsToken"}
)
