// Package testkit makes tiny test downloads: single-file torrents from local
// bytes (so a client that already has the data seeds at once, with no network)
// and minimal NZB files.
package testkit

import (
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- BitTorrent v1 pieces are SHA-1 by definition
	"encoding/hex"
	"fmt"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
)

// Torrent is a generated single-file torrent.
type Torrent struct {
	Name     string
	Data     []byte
	File     []byte // the .torrent
	InfoHash string
}

// MakeTorrent builds a private, trackerless single-file torrent for data.
func MakeTorrent(name string, data []byte) Torrent {
	const pieceLen = 16 << 10
	var pieces []byte
	for off := 0; off < len(data); off += pieceLen {
		end := min(off+pieceLen, len(data))
		sum := sha1.Sum(data[off:end]) // #nosec G401
		pieces = append(pieces, sum[:]...)
	}
	info := map[string]any{"name": name, "length": len(data), "piece length": pieceLen, "pieces": pieces, "private": 1}
	file := downloaders.Bencode(map[string]any{"announce": "http://127.0.0.1:1/announce", "created by": "locarr-test", "info": info})
	ti, err := downloaders.ParseTorrent(file)
	if err != nil {
		panic(err)
	}
	return Torrent{Name: name, Data: data, File: file, InfoHash: ti.InfoHash}
}

// RandomTorrent makes a torrent of n random bytes with a unique name.
func RandomTorrent(prefix string, n int) Torrent {
	data := make([]byte, n)
	_, _ = rand.Read(data)
	return MakeTorrent(fmt.Sprintf("%s-%s.bin", prefix, hex.EncodeToString(data[:4])), data)
}

// RandomMagnet returns a magnet link for a random info hash nobody seeds.
func RandomMagnet(name string) (link, infoHash string) {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	h := hex.EncodeToString(b)
	return "magnet:?xt=urn:btih:" + h + "&dn=" + name, h
}

// NZB returns a minimal valid NZB with one file of two segments.
func NZB(name string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
 <head><meta type="name">` + name + `</meta></head>
 <file poster="locarr@example.invalid" date="1700000000" subject="&quot;` + name + `.mkv&quot; yEnc (1/2)">
  <groups><group>alt.binaries.test</group></groups>
  <segments>
   <segment bytes="1000" number="1">` + name + `-part1@example.invalid</segment>
   <segment bytes="1000" number="2">` + name + `-part2@example.invalid</segment>
  </segments>
 </file>
</nzb>
`)
}

// TorrentFile is one file of a multi-file torrent (Path uses "/").
type TorrentFile struct {
	Path string
	Data []byte
}

// MakeFolderTorrent builds a private, trackerless multi-file torrent whose
// files live in the folder name (pieces span file boundaries, as the format
// defines).
func MakeFolderTorrent(name string, files []TorrentFile) Torrent {
	const pieceLen = 16 << 10
	var all []byte
	list := make([]any, 0, len(files))
	for _, f := range files {
		all = append(all, f.Data...)
		var parts []any
		for _, p := range splitPath(f.Path) {
			parts = append(parts, p)
		}
		list = append(list, map[string]any{"length": len(f.Data), "path": parts})
	}
	var pieces []byte
	for off := 0; off < len(all); off += pieceLen {
		end := min(off+pieceLen, len(all))
		sum := sha1.Sum(all[off:end]) // #nosec G401
		pieces = append(pieces, sum[:]...)
	}
	info := map[string]any{"name": name, "files": list, "piece length": pieceLen, "pieces": pieces, "private": 1}
	file := downloaders.Bencode(map[string]any{"announce": "http://127.0.0.1:1/announce", "created by": "locarr-test", "info": info})
	ti, err := downloaders.ParseTorrent(file)
	if err != nil {
		panic(err)
	}
	return Torrent{Name: name, Data: all, File: file, InfoHash: ti.InfoHash}
}

func splitPath(p string) []string {
	var out []string
	cur := ""
	for _, r := range p {
		if r == '/' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
