//go:build ignore

// generate writes the generated golden files (movies, tv, anime, music) from
// hand-picked titles and naming templates. The expected values come from the
// pieces each name is built from, never from the parser.
//
//	go run ./internal/parser/testdata/generate.go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type m = map[string]any

type golden struct {
	Name   string `json:"name"`
	Expect m      `json:"expect"`
}

func dots(s string) string { return strings.ReplaceAll(s, " ", ".") }

func merge(a, b m) m {
	out := m{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if sub, ok := v.(m); ok {
			if prev, ok := out[k].(m); ok {
				out[k] = merge(prev, sub)
				continue
			}
		}
		out[k] = v
	}
	return out
}

var groups = []string{"SPARKS", "FLUX", "NTb", "EDITH", "CMRG", "KiNGS", "playWEB", "TEPES", "SiGMA", "FGT", "GECKOS",
	"AMIABLE", "DRONES", "KOGi", "BAE", "MeGusta", "SuccessfulCrab", "ROVERS", "CtrlHD", "HiFi", "DON", "EbP", "LOL",
	"DIMENSION", "KILLERS", "AVS", "SVA", "FLEET", "MiNX", "BHDStudio", "NOGRP", "ETHEL", "GGEZ", "SYNCOPY", "W4NK3R",
	"BLOOM", "Kitsune", "PECULATE", "SMURF", "HONE", "WELP", "APEX", "TOMMY", "RAWR", "BYNDR", "NAISU", "STRiFE"}

type movie struct {
	title string
	year  int
}

var movies = []movie{
	{"1917", 2019}, {"Blade Runner 2049", 2017}, {"2001 A Space Odyssey", 1968}, {"Charlottes Web", 2006},
	{"The Matrix", 1999}, {"Mission Impossible Dead Reckoning Part One", 2023}, {"Spider-Man No Way Home", 2021},
	{"Mr Hollands Opus", 1995}, {"Oppenheimer", 2023}, {"Dune Part Two", 2024},
	{"The Lord of the Rings The Fellowship of the Ring", 2001}, {"Alien", 1979}, {"Se7en", 1995},
	{"Pans Labyrinth", 2006}, {"The Good the Bad and the Ugly", 1966}, {"Crouching Tiger Hidden Dragon", 2000},
	{"Fast X", 2023}, {"Up", 2009}, {"It", 2017}, {"Her", 2013}, {"Cam", 2018}, {"Ted 2", 2015},
	{"Toy Story 4", 2019}, {"Rocky IV", 1985}, {"The Italian Job", 2003}, {"The French Dispatch", 2021},
	{"Inglourious Basterds", 2009}, {"Leon The Professional", 1994}, {"Die Hard", 1988}, {"Tenet", 2020},
	{"Arrival", 2016}, {"Heat", 1995}, {"Avatar The Way of Water", 2022}, {"Everything Everywhere All at Once", 2022},
	{"Parasite", 2019}, {"The Batman", 2022}, {"Top Gun Maverick", 2022}, {"Knives Out", 2019},
	{"No Time to Die", 2021}, {"Fargo", 1996}, {"Joker", 2019}, {"Interstellar", 2014}, {"Gladiator II", 2024},
	{"Wonka", 2023}, {"John Wick Chapter 4", 2023}, {"Godzilla Minus One", 2023}, {"Poor Things", 2023},
	{"Past Lives", 2023}, {"Sicario", 2015}, {"The Thing", 1982}, {"WALL-E", 2008},
	{"X-Men Days of Future Past", 2014}, {"2012", 2009}, {"Apollo 13", 1995}, {"21 Jump Street", 2012},
	{"300", 2006}, {"10 Cloverfield Lane", 2016}, {"The 40 Year Old Virgin", 2005}, {"Super 8", 2011},
	{"District 9", 2009}, {"Brazil", 1985}, {"Australia", 2008}, {"Amelie", 2001}, {"Oceans Eleven", 2001},
	{"The Hateful Eight", 2015}, {"Kill Bill Vol 1", 2003}, {"Three Billboards Outside Ebbing Missouri", 2017},
	{"Once Upon a Time in Hollywood", 2019}, {"The Grand Budapest Hotel", 2014}, {"Prisoners", 2013},
}

type tmpl struct {
	f      func(t, g string, y int) string
	expect m
}

var movieTemplates = []tmpl{
	{func(t, g string, y int) string { return fmt.Sprintf("%s.%d.1080p.BluRay.x264-%s", dots(t), y, g) },
		m{"video": m{"source": "bluray", "resolution": 1080, "codec": "h264", "encoder": "x264"}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.2160p.UHD.BluRay.x265.10bit.HDR.DTS-HD.MA.5.1-%s", dots(t), y, g)
	}, m{"video": m{"source": "bluray", "uhd": true, "resolution": 2160, "codec": "h265", "bitDepth": 10, "hdr": []any{"HDR10"}},
		"audio": m{"codecs": []any{"dts-hd ma"}, "channels": "5.1"}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.1080p.AMZN.WEB-DL.DDP5.1.H.264-%s", dots(t), y, g)
	},
		m{"video": m{"source": "webdl", "resolution": 1080, "codec": "h264"}, "streamingService": "Amazon",
			"audio": m{"codecs": []any{"eac3"}, "channels": "5.1"}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.2160p.NF.WEB-DL.DDP5.1.Atmos.DV.HDR.H.265-%s", dots(t), y, g)
	}, m{"video": m{"source": "webdl", "resolution": 2160, "codec": "h265", "hdr": []any{"DV", "HDR10"}, "dolbyVisionProfile": "8"},
		"streamingService": "Netflix", "audio": m{"codecs": []any{"eac3"}, "channels": "5.1", "atmos": true}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1-%s", dots(t), y, g)
	}, m{"video": m{"source": "remux", "resolution": 1080, "codec": "h264"}, "audio": m{"codecs": []any{"dts-hd ma"}}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.2160p.BluRay.REMUX.HEVC.DV.HDR10.TrueHD.7.1.Atmos-%s", dots(t), y, g)
	}, m{"video": m{"source": "remux", "resolution": 2160, "codec": "h265", "hdr": []any{"DV", "HDR10"}, "dolbyVisionProfile": "7"},
		"audio": m{"codecs": []any{"truehd"}, "channels": "7.1", "atmos": true}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.1080p.Blu-ray.AVC.DTS-HD.MA.5.1-%s", dots(t), y, g)
	}, m{"video": m{"source": "brdisk", "resolution": 1080}}},
	{func(t, g string, y int) string { return fmt.Sprintf("%s.%d.HDCAM.x264-%s", dots(t), y, g) },
		m{"video": m{"source": "cam", "codec": "h264"}}},
	{func(t, g string, y int) string { return fmt.Sprintf("%s.%d.HDTS.x264.AAC-%s", dots(t), y, g) },
		m{"video": m{"source": "telesync"}, "audio": m{"codecs": []any{"aac"}}}},
	{func(t, g string, y int) string { return fmt.Sprintf("%s.%d.DVDSCR.XviD-%s", dots(t), y, g) },
		m{"video": m{"source": "screener", "codec": "xvid"}}},
	{func(t, g string, y int) string { return fmt.Sprintf("%s.%d.DVDRip.XviD.AC3-%s", dots(t), y, g) },
		m{"video": m{"source": "dvd", "codec": "xvid"}, "audio": m{"codecs": []any{"ac3"}}}},
	{func(t, g string, y int) string { return fmt.Sprintf("%s.%d.PAL.DVDR-%s", dots(t), y, g) },
		m{"video": m{"source": "dvdr"}, "flags": []any{"pal"}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.Directors.Cut.1080p.BluRay.x264-%s", dots(t), y, g)
	},
		m{"editions": []any{"Director's Cut"}, "video": m{"source": "bluray", "resolution": 1080}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.EXTENDED.REMASTERED.720p.BluRay.x264.DTS-%s", dots(t), y, g)
	}, m{"editions": []any{"Extended", "Remastered"}, "video": m{"resolution": 720}, "audio": m{"codecs": []any{"dts"}}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.IMAX.2160p.DSNP.WEB-DL.DDP5.1.Atmos.HDR10.H.265-%s", dots(t), y, g)
	}, m{"editions": []any{"IMAX"}, "streamingService": "Disney+", "video": m{"hdr": []any{"HDR10"}, "resolution": 2160}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.3D.HSBS.1080p.BluRay.x264-%s", dots(t), y, g)
	},
		m{"threeD": true, "video": m{"source": "bluray", "resolution": 1080}}},
	{func(t, g string, y int) string { return fmt.Sprintf("%s.%d.MULTi.1080p.BluRay.x264-%s", dots(t), y, g) },
		m{"multiAudio": true}},
	{func(t, g string, y int) string { return fmt.Sprintf("%s.%d.FRENCH.1080p.WEB.H264-%s", dots(t), y, g) },
		m{"languages": []any{"fr"}, "video": m{"source": "web", "codec": "h264"}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.iTALiAN.720p.BluRay.x264-%s", dots(t), y, g)
	},
		m{"languages": []any{"it"}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.German.DL.1080p.BluRay.x264-%s", dots(t), y, g)
	},
		m{"languages": []any{"de"}, "dualAudio": true}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.CROATIAN.1080p.WEB-DL.x264-%s", dots(t), y, g)
	},
		m{"languages": []any{"hr"}}},
	{func(t, g string, y int) string { return fmt.Sprintf("%s.%d.PROPER.1080p.WEB.h264-%s", dots(t), y, g) },
		m{"revision": m{"version": 2, "proper": true}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.REPACK.2160p.WEB-DL.x265-%s", dots(t), y, g)
	},
		m{"revision": m{"version": 2, "repack": true}, "video": m{"encoder": "x265"}}},
	{func(t, g string, y int) string { return fmt.Sprintf("%s.%d.720p.HDTV.x264-%s", dots(t), y, g) },
		m{"video": m{"source": "hdtv", "resolution": 720}}},
	{func(t, g string, y int) string { return fmt.Sprintf("%s.%d.1080p.WEBRip.x265-%s", dots(t), y, g) },
		m{"video": m{"source": "webrip", "resolution": 1080, "codec": "h265"}}},
	{func(t, g string, y int) string { return fmt.Sprintf("%s.%d.480p.DVDRip.x264.AAC2.0-%s", dots(t), y, g) },
		m{"video": m{"source": "dvd", "resolution": 480}, "audio": m{"channels": "2.0"}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.Hybrid.1080p.BluRay.DD+5.1.x264-%s", dots(t), y, g)
	},
		m{"flags": []any{"hybrid"}, "audio": m{"codecs": []any{"eac3"}, "channels": "5.1"}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.1080p.ATVP.WEB-DL.DDP5.1.Atmos.H.264-%s", dots(t), y, g)
	}, m{"streamingService": "Apple TV+", "audio": m{"atmos": true}}},
	{func(t, g string, y int) string {
		return strings.ToLower(fmt.Sprintf("%s.%d.1080p.bluray.x264-%s", dots(t), y, g))
	}, m{"video": m{"source": "bluray", "resolution": 1080}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s (%d) 2160p HMAX WEB-DL DV HDR10+ DDP 5.1 Atmos-%s", t, y, g)
	}, m{"streamingService": "HBO Max", "video": m{"hdr": []any{"DV", "HDR10+"}}, "audio": m{"codecs": []any{"eac3"}, "channels": "5.1", "atmos": true}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s (%d) (1080p BluRay x265 10bit AAC 5.1 Tigole)", t, y)
	}, m{"group": "Tigole", "video": m{"source": "bluray", "codec": "h265", "bitDepth": 10}}},
	{func(t, g string, y int) string { return fmt.Sprintf("%s (%d) [1080p] [WEBRip] [5.1] [YTS.MX]", t, y) },
		m{"group": "YTS.MX", "video": m{"source": "webrip", "resolution": 1080}, "audio": m{"channels": "5.1"}}},
	{func(t, g string, y int) string { return fmt.Sprintf("%s %d 1080p WEB-DL H264 AAC-%s", t, y, g) },
		m{"video": m{"source": "webdl", "codec": "h264"}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.UNRATED.720p.BluRay.x264-%s", dots(t), y, g)
	},
		m{"editions": []any{"Unrated"}}},
	{func(t, g string, y int) string { return fmt.Sprintf("%s.%d.1080p.WEB-DL.HC.HEVC-%s", dots(t), y, g) },
		m{"hardcodedSubs": true}},
	{func(t, g string, y int) string { return fmt.Sprintf("%s.%d.576p.PAL.BDRip.x264-%s", dots(t), y, g) },
		m{"video": m{"source": "bdrip", "resolution": 576}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.Open.Matte.1080p.WEB-DL.DD5.1.H.264-%s", dots(t), y, g)
	}, m{"editions": []any{"Open Matte"}, "audio": m{"codecs": []any{"ac3"}}}},
	{func(t, g string, y int) string {
		return fmt.Sprintf("%s.%d.Criterion.1080p.BluRay.FLAC.1.0.x264-%s", dots(t), y, g)
	},
		m{"editions": []any{"Criterion"}, "audio": m{"codecs": []any{"flac"}, "channels": "1.0"}, "kind": "movie"}},
}

type show struct {
	title string
	year  int // part of the name, 0 = none
}

var shows = []show{
	{"Breaking Bad", 0}, {"Game of Thrones", 0}, {"The Office US", 0}, {"Doctor Who", 2005}, {"Stranger Things", 0},
	{"The Last of Us", 0}, {"House of the Dragon", 0}, {"Mr Robot", 0}, {"9-1-1", 0}, {"24", 0}, {"Greys Anatomy", 0},
	{"The Boys", 0}, {"Fargo", 0}, {"Succession", 0}, {"Severance", 0}, {"Ted Lasso", 0}, {"The Bear", 0},
	{"Shogun", 2024}, {"Slow Horses", 0}, {"Only Murders in the Building", 0}, {"The Mandalorian", 0}, {"Andor", 0},
	{"Reacher", 0}, {"The Witcher", 0}, {"Dark", 0}, {"Money Heist", 0}, {"Squid Game", 0}, {"Bluey", 2018},
	{"Top Gear", 0}, {"The Simpsons", 0}, {"Law and Order SVU", 0}, {"Star Trek Discovery", 0}, {"Better Call Saul", 0},
	{"Peaky Blinders", 0}, {"Black Mirror", 0}, {"Westworld", 0}, {"Chernobyl", 0}, {"True Detective", 0},
	{"The Expanse", 0}, {"Foundation", 0}, {"Silo", 0}, {"Fallout", 0}, {"Loki", 0}, {"Wednesday", 0},
	{"Yellowstone", 0}, {"Ozark", 0}, {"Lost", 0}, {"Friends", 0}, {"Dexter", 0}, {"Battlestar Galactica", 2004},
	{"Room 104", 0}, {"Mob Psycho 100", 0}, {"The Rings of Power", 0}, {"Gen V", 0}, {"Kaos", 0},
}

var epTitles = []string{"Pilot", "The Long Night", "Chapter One", "Homecoming", "Winter Is Coming", "Pretty Much Dead Already",
	"The Rains of Castamere", "Ozymandias", "Fly", "Hard Times", "Long Long Time", "Inside Job"}

type tvT struct {
	f      func(t string, s, e int, g, et string) string
	expect func(s, e int, et string) m
}

func se(s, e int) m { return m{"seasons": []any{s}, "episodes": []any{e}, "kind": "series"} }

var tvTemplates = []tvT{
	{func(t string, s, e int, g, _ string) string {
		return fmt.Sprintf("%s.S%02dE%02d.720p.HDTV.x264-%s", dots(t), s, e, g)
	},
		func(s, e int, _ string) m { return merge(se(s, e), m{"video": m{"source": "hdtv", "resolution": 720}}) }},
	{func(t string, s, e int, g, _ string) string {
		return fmt.Sprintf("%s.S%02dE%02d.1080p.WEB.h264-%s", dots(t), s, e, g)
	},
		func(s, e int, _ string) m { return merge(se(s, e), m{"video": m{"source": "web", "resolution": 1080}}) }},
	{func(t string, s, e int, g, et string) string {
		return fmt.Sprintf("%s.S%02dE%02d.%s.1080p.AMZN.WEB-DL.DDP5.1.H.264-%s", dots(t), s, e, dots(et), g)
	}, func(s, e int, et string) m {
		return merge(se(s, e), m{"episodeTitle": et, "streamingService": "Amazon", "video": m{"source": "webdl"}})
	}},
	{func(t string, s, e int, g, _ string) string {
		return fmt.Sprintf("%s.S%02dE%02dE%02d.720p.HDTV.x264-%s", dots(t), s, e, e+1, g)
	}, func(s, e int, _ string) m { return m{"seasons": []any{s}, "episodes": []any{e, e + 1}} }},
	{func(t string, s, e int, g, _ string) string {
		return fmt.Sprintf("%s.S%02dE%02d-E%02d.1080p.WEB-DL.DD5.1.H.264-%s", dots(t), s, e, e+2, g)
	}, func(s, e int, _ string) m { return m{"seasons": []any{s}, "episodes": []any{e, e + 1, e + 2}} }},
	{func(t string, s, e int, g, _ string) string {
		return strings.ToLower(fmt.Sprintf("%s.%dx%02d.hdtv.xvid-%s", dots(t), s, e, g))
	}, func(s, e int, _ string) m { return merge(se(s, e), m{"video": m{"codec": "xvid"}}) }},
	{func(t string, s, e int, g, _ string) string {
		return fmt.Sprintf("%s.S%02d.1080p.BluRay.x264-%s", dots(t), s, g)
	},
		func(s, e int, _ string) m { return m{"seasons": []any{s}, "fullSeason": true, "episodes": []any{}} }},
	{func(t string, s, e int, g, _ string) string {
		return fmt.Sprintf("%s.Season.%d.1080p.WEB-DL.DD5.1.H.264-%s", dots(t), s, g)
	}, func(s, e int, _ string) m { return m{"seasons": []any{s}, "fullSeason": true} }},
	{func(t string, s, e int, g, _ string) string {
		return fmt.Sprintf("%s.S01-S%02d.COMPLETE.720p.BluRay.x264-%s", dots(t), s+1, g)
	}, func(s, e int, _ string) m {
		ss := []any{}
		for i := 1; i <= s+1; i++ {
			ss = append(ss, i)
		}
		return m{"seasons": ss, "multiSeason": true, "fullSeason": true}
	}},
	{func(t string, s, e int, g, _ string) string {
		return fmt.Sprintf("%s.S00E%02d.Special.720p.HDTV.x264-%s", dots(t), e, g)
	}, func(s, e int, _ string) m { return m{"seasons": []any{0}, "episodes": []any{e}, "special": true} }},
	{func(t string, s, e int, g, _ string) string {
		return fmt.Sprintf("%s.S%02dE%02d.REPACK.1080p.WEB.h264-%s", dots(t), s, e, g)
	}, func(s, e int, _ string) m { return merge(se(s, e), m{"revision": m{"repack": true, "version": 2}}) }},
	{func(t string, s, e int, g, _ string) string {
		return fmt.Sprintf("%s.S%02dE%02d.PROPER.720p.HDTV.x264-%s", dots(t), s, e, g)
	}, func(s, e int, _ string) m { return merge(se(s, e), m{"revision": m{"proper": true}}) }},
	{func(t string, s, e int, g, _ string) string {
		return fmt.Sprintf("%s.S%02dE%02d.MULTi.1080p.WEB.x264-%s", dots(t), s, e, g)
	}, func(s, e int, _ string) m { return merge(se(s, e), m{"multiAudio": true}) }},
	{func(t string, s, e int, g, _ string) string {
		return fmt.Sprintf("%s.S%02dE%02d.2160p.NF.WEB-DL.DDP5.1.HDR.HEVC-%s", dots(t), s, e, g)
	}, func(s, e int, _ string) m {
		return merge(se(s, e), m{"streamingService": "Netflix", "video": m{"hdr": []any{"HDR10"}, "resolution": 2160}})
	}},
	{func(t string, s, e int, g, _ string) string {
		return fmt.Sprintf("%s S%02dE%02d 1080p WEB H264-%s", t, s, e, g)
	},
		func(s, e int, _ string) m { return se(s, e) }},
	{func(t string, s, e int, g, _ string) string {
		return fmt.Sprintf("%s.S%02dE%02d.GERMAN.DL.1080p.WEB.h264-%s", dots(t), s, e, g)
	}, func(s, e int, _ string) m { return merge(se(s, e), m{"languages": []any{"de"}, "dualAudio": true}) }},
	{func(t string, s, e int, g, _ string) string {
		return fmt.Sprintf("%s.S%02dE%02d.HRV.720p.WEB-DL.x264-%s", dots(t), s, e, g)
	}, func(s, e int, _ string) m { return merge(se(s, e), m{"languages": []any{"hr"}}) }},
	{func(t string, s, e int, g, et string) string {
		return fmt.Sprintf("%s - S%02dE%02d - %s (1080p WEB-DL x265 %s)", t, s, e, et, g)
	}, func(s, e int, et string) m { return merge(se(s, e), m{"episodeTitle": et, "group": nil}) }},
	{func(t string, s, e int, g, _ string) string {
		return fmt.Sprintf("%s.S%02dE%02d.1080p.DSNP.WEB-DL.DDP5.1.H.264-%s[TGx]", dots(t), s, e, g)
	}, func(s, e int, _ string) m { return merge(se(s, e), m{"streamingService": "Disney+"}) }},
	{func(t string, s, e int, g, _ string) string {
		return fmt.Sprintf("%s.S%02dE%02d.720p.WEB.x265-%s[eztv]", dots(t), s, e, g)
	}, func(s, e int, _ string) m { return se(s, e) }},
	{func(t string, s, e int, g, _ string) string {
		return fmt.Sprintf("%s.S%02dE%02d.1080p.ATVP.WEB-DL.DDP5.1.Atmos.DV.H.265-%s", dots(t), s, e, g)
	}, func(s, e int, _ string) m {
		return merge(se(s, e), m{"video": m{"hdr": []any{"DV"}, "dolbyVisionProfile": "5"}, "streamingService": "Apple TV+"})
	}},
}

type dailyShow struct{ title string }

var daily = []string{"The Daily Show", "Last Week Tonight with John Oliver", "The Tonight Show Starring Jimmy Fallon",
	"Jeopardy", "WWE Monday Night RAW", "Late Night with Seth Meyers", "The Late Show with Stephen Colbert",
	"Real Time with Bill Maher", "Dnevnik HRT", "Good Mythical Morning"}

var guests = []string{"Margot Robbie", "Ryan Gosling", "", "Zendaya", "", "Keanu Reeves"}

type anime struct {
	title string
	group string
}

var animeShows = []anime{
	{"Jujutsu Kaisen", "SubsPlease"}, {"One Piece", "Erai-raws"}, {"Sousou no Frieren", "SubsPlease"},
	{"Spy x Family", "EMBER"}, {"Chainsaw Man", "Judas"}, {"Attack on Titan", "HorribleSubs"},
	{"Demon Slayer Kimetsu no Yaiba", "ASW"}, {"Bocchi the Rock!", "Commie"}, {"Mob Psycho 100", "DKB"},
	{"Dr Stone", "SubsPlease"}, {"Vinland Saga", "Yameii"}, {"Blue Lock", "EMBER"}, {"Dandadan", "SubsPlease"},
	{"Solo Leveling", "Erai-raws"}, {"Kaiju No 8", "ASW"}, {"Kusuriya no Hitorigoto", "SubsPlease"},
	{"Oshi no Ko", "Judas"}, {"Made in Abyss", "Anime Time"},
}

var hashes = []string{"A1B2C3D4", "9F3E2C1B", "DEADBEEF", "0BADF00D", "C0FFEE42", "7E4A1D90", "12AB34CD", "FA5B7C3E"}

type album struct {
	artist, album string
	year          int
}

var albums = []album{
	{"Pink Floyd", "The Dark Side of the Moon", 1973}, {"Daft Punk", "Random Access Memories", 2013},
	{"Radiohead", "OK Computer", 1997}, {"Taylor Swift", "1989", 2014}, {"Kendrick Lamar", "To Pimp a Butterfly", 2015},
	{"Miles Davis", "Kind of Blue", 1959}, {"Fleetwood Mac", "Rumours", 1977}, {"Massive Attack", "Mezzanine", 1998},
	{"Bjork", "Homogenic", 1997}, {"Dino Merlin", "Hotel Nacional", 2014}, {"Gibonni", "Mirakul", 2001},
	{"Prljavo Kazaliste", "Crno bijeli svijet", 1980}, {"Beyonce", "Lemonade", 2016}, {"Adele", "25", 2015},
	{"Metallica", "Master of Puppets", 1986}, {"Nirvana", "Nevermind", 1991}, {"The Beatles", "Abbey Road", 1969},
	{"Billie Eilish", "Hit Me Hard and Soft", 2024}, {"Arctic Monkeys", "AM", 2013},
	{"Boards of Canada", "Music Has the Right to Children", 1998}, {"Queen", "A Night at the Opera", 1975},
	{"Parni Valjak", "Sjaj u tami", 1983},
}

func main() {
	dir := filepath.Join("internal", "parser", "testdata")
	n := 0
	// movies: every movie gets 6 templates, rotating through the list
	var out []golden
	for i, mv := range movies {
		for j := 0; j < 6; j++ {
			tp := movieTemplates[(i*5+j*7)%len(movieTemplates)]
			g := groups[(i*3+j)%len(groups)]
			name := tp.f(mv.title, g, mv.year)
			title := mv.title
			if name == strings.ToLower(name) {
				title, g = strings.ToLower(title), strings.ToLower(g)
			}
			exp := merge(m{"title": title, "year": mv.year, "group": g, "unparsed": []any{}}, tp.expect)
			if strings.Contains(name, mv.title+" (") || strings.HasPrefix(name, mv.title+" ") {
				// spaced names keep the title as written
			}
			if _, ok := exp["kind"]; !ok {
				exp["kind"] = "movie"
			}
			out = append(out, golden{name, exp})
		}
	}
	write(dir, "movies.json", out)
	n += len(out)

	out = nil
	for i, sh := range shows {
		for j := 0; j < 7; j++ {
			tp := tvTemplates[(i*3+j*5)%len(tvTemplates)]
			g := groups[(i*7+j*3)%len(groups)]
			s, e := 1+(i+j)%6, 1+(i*j+3)%20
			et := epTitles[(i+j)%len(epTitles)]
			t := sh.title
			if sh.year > 0 {
				t = fmt.Sprintf("%s %d", sh.title, sh.year)
			}
			name := tp.f(t, s, e, g, et)
			exp := merge(m{"title": sh.title, "group": g, "kind": "series", "unparsed": []any{}}, tp.expect(s, e, et))
			if sh.year > 0 {
				exp["year"] = sh.year
			}
			if name == strings.ToLower(name) {
				exp["title"] = strings.ToLower(sh.title)
				exp["group"] = strings.ToLower(g)
			}
			if exp["group"] == nil {
				exp["group"] = g // spaced "(… x265 GROUP)" form: the last bracketed word
			}
			out = append(out, golden{name, exp})
		}
	}
	// daily shows
	for i, d := range daily {
		for j := 0; j < 4; j++ {
			y, mo, dd := 2019+(i+j)%6, 1+(i*3+j)%12, 1+(i*7+j*5)%28
			g := groups[(i+j*11)%len(groups)]
			guest := guests[(i+j)%len(guests)]
			var name string
			exp := m{"title": d, "airDate": fmt.Sprintf("%04d-%02d-%02d", y, mo, dd), "kind": "series", "group": g, "unparsed": []any{}}
			switch j % 4 {
			case 0:
				name = fmt.Sprintf("%s.%04d.%02d.%02d.%s.720p.WEB.h264-%s", dots(d), y, mo, dd, dots(guest), g)
				if guest != "" {
					exp["episodeTitle"] = guest
				}
				name = strings.ReplaceAll(name, "..", ".")
			case 1:
				name = fmt.Sprintf("%s %04d-%02d-%02d 1080p HDTV H264-%s", d, y, mo, dd, g)
			case 2:
				name = fmt.Sprintf("%s.%04d.%02d.%02d.1080p.PCOK.WEB-DL.AAC2.0.H.264-%s", dots(d), y, mo, dd, g)
				exp["streamingService"] = "Peacock"
			default:
				name = fmt.Sprintf("%s.%02d.%02d.%04d.720p.HDTV.x264-%s", dots(d), dd, mo, y, g)
			}
			out = append(out, golden{name, exp})
		}
	}
	write(dir, "tv.json", out)
	n += len(out)

	out = nil
	for i, a := range animeShows {
		h := hashes[i%len(hashes)]
		e := 1 + (i*7)%24
		base := m{"title": a.title, "group": a.group, "kind": "series", "anime": true, "unparsed": []any{}}
		add := func(name string, exp m) { out = append(out, golden{name, merge(base, exp)}) }
		add(fmt.Sprintf("[%s] %s - %02d (1080p) [%s].mkv", a.group, a.title, e, h),
			m{"absoluteEpisodes": []any{e}, "hash": h, "extension": "mkv", "video": m{"resolution": 1080}})
		add(fmt.Sprintf("[%s] %s - %02dv2 (720p) [%s].mkv", a.group, a.title, e+1, h),
			m{"absoluteEpisodes": []any{e + 1}, "revision": m{"version": 2}, "video": m{"resolution": 720}})
		add(fmt.Sprintf("[%s] %s - %03d [1080p][HEVC x265 10bit][Multi-Subs]", a.group, a.title, e+100),
			m{"absoluteEpisodes": []any{e + 100}, "multiSubs": true, "video": m{"codec": "h265", "bitDepth": 10}})
		add(fmt.Sprintf("[%s] %s (01-12) [1080p] [Batch]", a.group, a.title),
			m{"absoluteEpisodes": []any{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}})
		add(fmt.Sprintf("[%s] %s S2 - %02d [1080p]", a.group, a.title, e),
			m{"seasons": []any{2}, "episodes": []any{e}, "anime": true})
		add(fmt.Sprintf("[%s] %s [%02d][1080p][x265].mkv", a.group, a.title, e+2),
			m{"absoluteEpisodes": []any{e + 2}})
		g := groups[i%len(groups)]
		out = append(out, golden{fmt.Sprintf("%s.S01E%02d.1080p.CR.WEB-DL.AAC2.0.H.264-%s", dots(a.title), e, g),
			m{"title": a.title, "group": g, "seasons": []any{1}, "episodes": []any{e}, "streamingService": "Crunchyroll", "unparsed": []any{}}})
	}
	for _, e := range []int{1012, 1100, 1088} {
		out = append(out, golden{fmt.Sprintf("One.Piece.E%d.1080p.WEB.H264-SKYANiME", e),
			m{"title": "One Piece", "absoluteEpisodes": []any{e}, "group": "SKYANiME"}})
	}
	write(dir, "anime.json", out)
	n += len(out)

	out = nil
	for i, a := range albums {
		g := groups[(i*5)%len(groups)]
		base := m{"title": a.album, "year": a.year, "kind": "music", "music": m{"artist": a.artist, "album": a.album}}
		add := func(name string, exp m) { out = append(out, golden{name, merge(base, exp)}) }
		add(fmt.Sprintf("%s - %s (%d) [FLAC]", a.artist, a.album, a.year), m{"audio": m{"codecs": []any{"flac"}}})
		add(fmt.Sprintf("%s - %s (%d) [FLAC 24bit-96kHz]", a.artist, a.album, a.year),
			m{"audio": m{"codecs": []any{"flac"}, "bitDepth": 24, "sampleRateKHz": 96}})
		add(fmt.Sprintf("%s - %s (%d) [MP3 320]", a.artist, a.album, a.year), m{"audio": m{"codecs": []any{"mp3"}, "bitrate": "320"}})
		add(fmt.Sprintf("%s - %s [%d] [MP3 V0]", a.artist, a.album, a.year), m{"audio": m{"codecs": []any{"mp3"}, "bitrate": "V0"}})
		u := func(s string) string { return strings.ReplaceAll(s, " ", "_") }
		add(fmt.Sprintf("%s-%s-WEB-%d-%s", u(a.artist), u(a.album), a.year, g), m{"group": g, "music": m{"source": "web"}})
		add(fmt.Sprintf("%s-%s-2CD-%d-%s", u(a.artist), u(a.album), a.year, g), m{"group": g, "music": m{"discs": 2, "source": "cd"}})
		add(fmt.Sprintf("%s-%s-(CAT%04d)-CD-FLAC-%d-%s", u(a.artist), u(a.album), 100+i, a.year, g),
			m{"group": g, "audio": m{"codecs": []any{"flac"}}, "music": m{"source": "cd"}})
		add(fmt.Sprintf("%s - %s (%d) [WEB FLAC 16-44]", a.artist, a.album, a.year),
			m{"audio": m{"codecs": []any{"flac"}, "bitDepth": 16, "sampleRateKHz": 44.1}})
	}
	write(dir, "music.json", out)
	n += len(out)
	fmt.Println("generated", n, "names")
}

func write(dir, file string, out []golden) {
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), append(b, '\n'), 0o644); err != nil {
		panic(err)
	}
}
