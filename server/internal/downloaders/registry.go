package downloaders

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Field names a connection setting a client needs; the web form shows only these.
type Field string

// Connection fields.
const (
	FieldHost            Field = "host"
	FieldPort            Field = "port"
	FieldUseTLS          Field = "useTls"
	FieldURLBase         Field = "urlBase"
	FieldUsername        Field = "username"
	FieldPassword        Field = "password"
	FieldAPIKey          Field = "apiKey"
	FieldDirectory       Field = "directory"
	FieldAllowSelfSigned Field = "allowSelfSigned"
)

// Config is how to reach one client.
type Config struct {
	Host     string
	Port     int
	UseTLS   bool
	URLBase  string
	Username string
	Password string
	APIKey   string
	// Directory is the client's download folder as the client sees it (aria2,
	// whose categories are sub-folders of it; optional elsewhere).
	Directory       string
	AllowSelfSigned bool
	// HTTP is the guarded HTTP client to use (NewHTTPClient); nil = a default one.
	HTTP *http.Client
}

// BaseURL returns scheme://host:port plus the URL base (without a trailing slash).
func (c Config) BaseURL() (*url.URL, error) {
	host := strings.TrimSpace(c.Host)
	if host == "" {
		return nil, fmt.Errorf("%w: host is required", ErrInvalidRequest)
	}
	if strings.ContainsAny(host, "/?#@ ") {
		return nil, fmt.Errorf("%w: host must be a name or an address, not a URL", ErrInvalidRequest)
	}
	if c.Port < 1 || c.Port > 65535 {
		return nil, fmt.Errorf("%w: port must be 1-65535", ErrInvalidRequest)
	}
	scheme := "http"
	if c.UseTLS {
		scheme = "https"
	}
	base := strings.Trim(strings.TrimSpace(c.URLBase), "/")
	if strings.ContainsAny(base, "?#") || strings.Contains(base, "..") {
		return nil, fmt.Errorf("%w: invalid URL base", ErrInvalidRequest)
	}
	u := &url.URL{Scheme: scheme, Host: net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(c.Port))}
	if base != "" {
		u.Path = "/" + base
	}
	return u, nil
}

// Client returns the HTTP client to use.
func (c Config) Client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return NewHTTPClient(HTTPOptions{AllowSelfSigned: c.AllowSelfSigned})
}

// Implementation describes one supported client.
type Implementation struct {
	// Name is the stable identifier stored with each client ("qbittorrent").
	Name        string
	DisplayName string
	Protocol    Protocol
	DefaultPort int
	// DefaultURLBase is pre-filled in the form ("/transmission/rpc" style bases are added by the client itself).
	DefaultURLBase string
	DefaultUser    string
	Fields         []Field
	// Required fields must be non-empty.
	Required     []Field
	Capabilities Capabilities
	New          func(Config) (Client, error)
}

var (
	regMu sync.RWMutex
	reg   = map[string]Implementation{}
)

// Register adds an implementation (called from each client's init).
func Register(i Implementation) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := reg[i.Name]; dup {
		panic("downloaders: duplicate implementation " + i.Name)
	}
	reg[i.Name] = i
}

// Lookup returns the implementation called name.
func Lookup(name string) (Implementation, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	i, ok := reg[name]
	return i, ok
}

// Implementations returns every registered implementation, torrent first, by name.
func Implementations() []Implementation {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Implementation, 0, len(reg))
	for _, i := range reg {
		out = append(out, i)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Protocol != out[b].Protocol {
			return out[a].Protocol == Torrent
		}
		return out[a].Name < out[b].Name
	})
	return out
}

// HasField reports whether the implementation uses f.
func (i Implementation) HasField(f Field) bool {
	for _, x := range i.Fields {
		if x == f {
			return true
		}
	}
	return false
}
