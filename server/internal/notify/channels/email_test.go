package channels

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"
)

// smtpServer is a tiny SMTP server for tests: EHLO, STARTTLS, AUTH PLAIN,
// MAIL, RCPT, DATA and QUIT.
type smtpServer struct {
	ln       net.Listener
	tls      *tls.Config
	implicit bool
	mu       sync.Mutex
	auth     string
	from     string
	rcpt     []string
	data     string
	sawTLS   bool
	done     chan struct{}
}

func selfSigned(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "mail.test"}, DNSNames: []string{"mail.test"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}, pool
}

func startSMTP(t *testing.T, srvTLS *tls.Config, implicit bool) *smtpServer {
	t.Helper()
	var ln net.Listener
	var err error
	if implicit {
		ln, err = tls.Listen("tcp", "127.0.0.1:0", srvTLS)
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatal(err)
	}
	s := &smtpServer{ln: ln, tls: srvTLS, implicit: implicit, done: make(chan struct{})}
	t.Cleanup(func() { _ = ln.Close() })
	go s.serve()
	return s
}

func (s *smtpServer) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *smtpServer) serve() {
	defer close(s.done)
	c, err := s.ln.Accept()
	if err != nil {
		return
	}
	defer c.Close()
	s.mu.Lock()
	s.sawTLS = s.implicit
	s.mu.Unlock()
	r, w := bufio.NewReader(c), c
	say := func(line string) { _, _ = io.WriteString(w, line+"\r\n") }
	say("220 mail.test ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch cmd {
		case "EHLO", "HELO":
			s.mu.Lock()
			secure := s.sawTLS
			s.mu.Unlock()
			if s.tls != nil && !secure {
				say("250-mail.test")
				say("250-STARTTLS")
				say("250 AUTH PLAIN")
			} else {
				say("250-mail.test")
				say("250 AUTH PLAIN")
			}
		case "STARTTLS":
			say("220 go ahead")
			tc := tls.Server(c, s.tls)
			if err := tc.Handshake(); err != nil {
				return
			}
			c = tc
			r, w = bufio.NewReader(tc), tc
			s.mu.Lock()
			s.sawTLS = true
			s.mu.Unlock()
		case "AUTH":
			parts := strings.Fields(line)
			if len(parts) == 3 {
				b, _ := base64.StdEncoding.DecodeString(parts[2])
				s.mu.Lock()
				s.auth = string(b)
				s.mu.Unlock()
			}
			say("235 ok")
		case "MAIL":
			s.mu.Lock()
			s.from = line
			s.mu.Unlock()
			say("250 ok")
		case "RCPT":
			s.mu.Lock()
			s.rcpt = append(s.rcpt, line)
			s.mu.Unlock()
			say("250 ok")
		case "DATA":
			say("354 send")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			s.mu.Lock()
			s.data = b.String()
			s.mu.Unlock()
			say("250 queued")
		case "QUIT":
			say("221 bye")
			return
		default:
			say("502 unknown")
		}
	}
}

func checkMail(t *testing.T, s *smtpServer) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("smtp server did not finish")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !strings.Contains(s.from, "<locarr@example.com>") || len(s.rcpt) != 2 || !strings.Contains(s.rcpt[1], "<ivo@example.com>") {
		t.Fatalf("envelope %q %q", s.from, s.rcpt)
	}
	m, err := mail.ReadMessage(strings.NewReader(s.data))
	if err != nil {
		t.Fatal(err)
	}
	subj, err := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
	if err != nil || subj != "Sada dostupno: Čudesna šuma (2020)" {
		t.Fatalf("subject %q (raw %q) %v", subj, m.Header.Get("Subject"), err)
	}
	if !strings.HasPrefix(m.Header.Get("Subject"), "=?utf-8?") || m.Header.Get("Message-Id") == "" || m.Header.Get("Date") == "" ||
		m.Header.Get("Mime-Version") != "1.0" {
		t.Fatalf("headers %v", m.Header)
	}
	mt, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/alternative" {
		t.Fatalf("content type %q", m.Header.Get("Content-Type"))
	}
	mr := multipart.NewReader(m.Body, params["boundary"])
	var types []string
	var html string
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(p) // multipart decodes quoted-printable
		types = append(types, strings.SplitN(p.Header.Get("Content-Type"), ";", 2)[0])
		if strings.HasPrefix(p.Header.Get("Content-Type"), "text/html") {
			html = string(b)
		} else if !strings.Contains(string(b), "Otvori u Locarru: http://locarr.test/requests/7") {
			t.Fatalf("plain part %q", b)
		}
	}
	if strings.Join(types, ",") != "text/plain,text/html" {
		t.Fatalf("parts %v", types)
	}
	if !strings.Contains(html, "&lt;script&gt;") || strings.Contains(html, "<script>") ||
		!strings.Contains(html, `<img src="https://image.tmdb.org/t/p/w342/p.jpg"`) || !strings.Contains(html, `href="http://locarr.test/requests/7"`) {
		t.Fatalf("html %q", html)
	}
}

func mailMessage() Message {
	m := sampleMessage()
	m = Compose(m.Event, "hr", "http://locarr.test")
	m.Headline = "Sada dostupno: Čudesna šuma (2020)"
	m.Body = "Komentar: <script>alert(1)</script>"
	m.PosterURL = "https://image.tmdb.org/t/p/w342/p.jpg"
	return m
}

var mailTo = Config{To: []string{"Ana <ana@example.com>", "ivo@example.com"}, Language: "hr"}

func TestEmailPlainLoopback(t *testing.T) {
	s := startSMTP(t, nil, false)
	env := Env{SMTP: SMTP{Host: "127.0.0.1", Port: s.port(), Security: "none", Username: "u", Password: "pw-secret", From: "Locarr <locarr@example.com>"}}
	if err := Send(context.Background(), Email, mailTo, Secrets{}, env, mailMessage()); err != nil {
		t.Fatal(err)
	}
	checkMail(t, s)
	if s.auth != "\x00u\x00pw-secret" {
		t.Fatalf("auth %q", s.auth)
	}
}

func TestEmailStartTLS(t *testing.T) {
	srv, pool := selfSigned(t)
	s := startSMTP(t, srv, false)
	env := Env{SMTP: SMTP{Host: "127.0.0.1", Port: s.port(), Security: "starttls", Username: "u", Password: "pw", From: "locarr@example.com"},
		TLSConfig: &tls.Config{RootCAs: pool, ServerName: "mail.test", MinVersion: tls.VersionTLS12}}
	if err := Send(context.Background(), Email, mailTo, Secrets{}, env, mailMessage()); err != nil {
		t.Fatal(err)
	}
	checkMail(t, s)
	if !s.sawTLS || s.auth != "\x00u\x00pw" {
		t.Fatalf("tls %v auth %q", s.sawTLS, s.auth)
	}
}

func TestEmailImplicitTLS(t *testing.T) {
	srv, pool := selfSigned(t)
	s := startSMTP(t, srv, true)
	env := Env{SMTP: SMTP{Host: "127.0.0.1", Port: s.port(), Security: "tls", From: "locarr@example.com"},
		TLSConfig: &tls.Config{RootCAs: pool, ServerName: "mail.test", MinVersion: tls.VersionTLS12}}
	if err := Send(context.Background(), Email, mailTo, Secrets{}, env, mailMessage()); err != nil {
		t.Fatal(err)
	}
	checkMail(t, s)
	if s.auth != "" {
		t.Fatalf("auth without a user name: %q", s.auth)
	}
}

func TestEmailRefusals(t *testing.T) {
	// STARTTLS required but not offered.
	s := startSMTP(t, nil, false)
	env := Env{SMTP: SMTP{Host: "127.0.0.1", Port: s.port(), From: "locarr@example.com", Password: "pw-secret"}, SMTPTimeout: 5 * time.Second}
	err := Send(context.Background(), Email, mailTo, Secrets{}, env, mailMessage())
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("err %v", err)
	}
	// No mail server, bad sender.
	if err := Send(context.Background(), Email, mailTo, Secrets{}, Env{}, mailMessage()); err == nil {
		t.Fatal("sent without a mail server")
	}
	if err := Send(context.Background(), Email, mailTo, Secrets{}, Env{SMTP: SMTP{Host: "h", From: "x\r\nBcc: y@z"}}, mailMessage()); err == nil {
		t.Fatal("header injection in the sender accepted")
	}
	// A subject with a line break stays one header line.
	b, err := buildMail(&mail.Address{Address: "a@b.test"}, []string{"c@d.test"}, Message{Headline: "x\r\nBcc: e@f.test"}, "en", time.Now())
	head, _, _ := strings.Cut(string(b), "\r\n\r\n")
	if err != nil || strings.Contains(head, "\r\nBcc:") || !strings.Contains(head, "Subject: x Bcc: e@f.test\r\n") {
		t.Fatalf("subject injection: %v %q", err, b)
	}
}
