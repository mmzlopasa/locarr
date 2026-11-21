package downloaders_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/downloaders/testkit"
)

func TestParseTorrentAndMagnet(t *testing.T) {
	tor := testkit.MakeTorrent("file.bin", []byte(strings.Repeat("x", 40000)))
	ti, err := downloaders.ParseTorrent(tor.File)
	if err != nil || ti.InfoHash != tor.InfoHash || ti.Name != "file.bin" || ti.Size != 40000 || len(ti.InfoHash) != 40 {
		t.Fatalf("ParseTorrent = %+v, %v", ti, err)
	}
	for _, bad := range [][]byte{nil, []byte("hello"), []byte("d4:infoi1ee"), []byte("d4:info"), []byte("<html>")} {
		if _, err := downloaders.ParseTorrent(bad); err == nil {
			t.Errorf("ParseTorrent(%q) accepted", bad)
		}
	}
	m, err := downloaders.ParseMagnet("magnet:?xt=urn:btih:C12FE1C06BBA254A9DC9F519B335AA7C1367A88A&dn=Name")
	if err != nil || m.InfoHash != "c12fe1c06bba254a9dc9f519b335aa7c1367a88a" || m.Name != "Name" {
		t.Fatalf("hex magnet = %+v, %v", m, err)
	}
	b32, err := downloaders.ParseMagnet("magnet:?xt=urn:btih:YEX6DQDLXISUVHOJ6UM3GNNKPQJWPKEK")
	if err != nil || b32.InfoHash != "c12fe1c06bba254a9dc9f519b335aa7c1367a88a" {
		t.Fatalf("base32 magnet = %+v, %v", b32, err)
	}
	for _, bad := range []string{"http://x", "magnet:?dn=x", "magnet:?xt=urn:btih:zz"} {
		if _, err := downloaders.ParseMagnet(bad); err == nil {
			t.Errorf("ParseMagnet(%q) accepted", bad)
		}
	}
}

func TestAllowedAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"192.168.1.10": true, "10.0.0.5": true, "172.18.0.3": true, "127.0.0.1": true, "8.8.8.8": true, "::1": true,
		"169.254.169.254": false, "169.254.10.1": false, "fe80::1": false, "0.0.0.0": false, "224.0.0.1": false,
		"fd00:ec2::254": false, "::ffff:169.254.169.254": false,
	} {
		if got := downloaders.AllowedAddr(netip.MustParseAddr(addr), false); got != want {
			t.Errorf("AllowedAddr(%s) = %v, want %v", addr, got, want)
		}
	}
	if !downloaders.AllowedAddr(netip.MustParseAddr("169.254.10.1"), true) {
		t.Error("link-local not allowed with the option")
	}
	if downloaders.AllowedAddr(netip.MustParseAddr("169.254.169.254"), true) {
		t.Error("metadata address allowed")
	}
}

func TestBaseURL(t *testing.T) {
	u, err := downloaders.Config{Host: "qbit", Port: 8080, UseTLS: true, URLBase: "/qb/"}.BaseURL()
	if err != nil || u.String() != "https://qbit:8080/qb" {
		t.Fatalf("BaseURL = %v, %v", u, err)
	}
	u, err = downloaders.Config{Host: "::1", Port: 1}.BaseURL()
	if err != nil || u.String() != "http://[::1]:1" {
		t.Fatalf("IPv6 BaseURL = %v, %v", u, err)
	}
	for _, c := range []downloaders.Config{{Host: "", Port: 1}, {Host: "http://x", Port: 1}, {Host: "x", Port: 0},
		{Host: "x", Port: 1, URLBase: "../a"}, {Host: "u:p@x", Port: 1}} {
		if _, err := c.BaseURL(); !errors.Is(err, downloaders.ErrInvalidRequest) {
			t.Errorf("%+v accepted", c)
		}
	}
}

func TestHTTPClientGuards(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("other")) }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/same":
			http.Redirect(w, r, "/ok", http.StatusFound)
		case "/cross":
			http.Redirect(w, r, strings.Replace(other.URL, "127.0.0.1", "localhost", 1), http.StatusFound)
		default:
			_, _ = w.Write([]byte("ok"))
		}
	}))
	defer srv.Close()
	hc := downloaders.NewHTTPClient(downloaders.HTTPOptions{})
	get := func(u string) (*http.Response, error) {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
		return hc.Do(req)
	}
	res, err := get(srv.URL + "/same")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	_, err = get(srv.URL + "/cross")
	var redir *downloaders.CrossHostRedirect
	if !errors.As(err, &redir) {
		t.Fatalf("cross-host redirect followed: %v", err)
	}
	_, err = get("http://169.254.169.254/latest")
	if !errors.Is(downloaders.ClassifyTransportError(err), downloaders.ErrBlocked) {
		t.Fatalf("metadata address: %v", err)
	}
	if _, err := downloaders.ReadLimited(strings.NewReader("12345"), 4); !errors.Is(err, downloaders.ErrResponseTooLarge) {
		t.Fatalf("ReadLimited: %v", err)
	}
	err = downloaders.ClassifyTransportError(errors.New(`Get "http://h/api?apikey=SECRET": dial tcp: refused`))
	if !errors.Is(err, downloaders.ErrUnreachable) {
		t.Fatal(err)
	}
}

func TestAddRequestValidate(t *testing.T) {
	r := downloaders.AddRequest{Magnet: "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)}
	if err := r.Validate(downloaders.Torrent); err != nil || r.Priority != downloaders.PriorityNormal {
		t.Fatal(err)
	}
	if err := r.Validate(downloaders.Usenet); err == nil {
		t.Fatal("magnet accepted by a usenet client")
	}
	two := downloaders.AddRequest{Magnet: "m", NZB: []byte("x")}
	if err := two.Validate(downloaders.Torrent); err == nil {
		t.Fatal("two sources accepted")
	}
}

func TestImplementationsRegistered(t *testing.T) {
	// The registry holds only what is imported; the downloads/app tests cover the full set.
	if _, ok := downloaders.Lookup("nope"); ok {
		t.Fatal("unknown implementation found")
	}
}
