// Package all registers every download client implementation.
package all

import (
	// Each package registers itself with downloaders.Register.
	_ "github.com/mmzlopasa/locarr/server/internal/downloaders/aria2"
	_ "github.com/mmzlopasa/locarr/server/internal/downloaders/deluge"
	_ "github.com/mmzlopasa/locarr/server/internal/downloaders/nzbget"
	_ "github.com/mmzlopasa/locarr/server/internal/downloaders/qbittorrent"
	_ "github.com/mmzlopasa/locarr/server/internal/downloaders/rtorrent"
	_ "github.com/mmzlopasa/locarr/server/internal/downloaders/sabnzbd"
	_ "github.com/mmzlopasa/locarr/server/internal/downloaders/transmission"
)
