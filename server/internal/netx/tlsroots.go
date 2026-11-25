package netx

import (
	"crypto/tls"
	"crypto/x509"
	"embed"
	"net/http"
	"sync"
)

// Let's Encrypt's Generation Y roots (ISRG Root YE and YR, 2025). Some hosts
// serve chains that end at them without the cross-signature to ISRG Root
// X1/X2, and the system bundle of the runtime image (Debian trixie) does not
// carry them yet, so such hosts fail with "certificate signed by unknown
// authority" (ThePornDB's image host *.metadataapi.net does). The files are
// the public roots from letsencrypt.org/certificates.
//
//go:embed roots/*.pem
var extraRoots embed.FS

var (
	rootsOnce sync.Once
	rootsPool *x509.CertPool
)

// RootCAs is the system pool plus the extra public roots above.
func RootCAs() *x509.CertPool {
	rootsOnce.Do(func() {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		entries, _ := extraRoots.ReadDir("roots")
		for _, e := range entries {
			if b, err := extraRoots.ReadFile("roots/" + e.Name()); err == nil {
				pool.AppendCertsFromPEM(b)
			}
		}
		rootsPool = pool
	})
	return rootsPool
}

// Transport is a clone of the default transport that trusts RootCAs. The
// pool (system bundle included) is loaded on the first request, not at
// start-up.
func Transport() http.RoundTripper { return &lazyTransport{} }

type lazyTransport struct {
	once sync.Once
	t    *http.Transport
}

func (l *lazyTransport) transport() *http.Transport {
	l.once.Do(func() {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: RootCAs()}
		l.t = t
	})
	return l.t
}

// RoundTrip implements http.RoundTripper.
func (l *lazyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return l.transport().RoundTrip(r)
}
