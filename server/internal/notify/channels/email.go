package channels

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"html"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
)

type emailSender struct{}

func (emailSender) Validate(cfg Config, _ Secrets) error { return validAddresses(cfg.To) }

// checkSMTP validates the mail server settings.
func checkSMTP(s SMTP) error {
	if strings.TrimSpace(s.Host) == "" {
		return invalid("the mail server is not set up (Settings → Notifications)")
	}
	switch s.Security {
	case "", "starttls", "tls", "none":
	default:
		return invalid("mail server security must be none, starttls or tls")
	}
	if s.Port < 0 || s.Port > 65535 {
		return invalid("mail server port is out of range")
	}
	if strings.ContainsAny(s.From, "\r\n") {
		return invalid("sender address is not an e-mail address")
	}
	if _, err := mail.ParseAddress(s.From); err != nil {
		return invalid("sender address is not an e-mail address")
	}
	return nil
}

func smtpPort(s SMTP) int {
	if s.Port > 0 {
		return s.Port
	}
	switch s.Security {
	case "tls":
		return 465
	case "none":
		return 25
	}
	return 587
}

func (emailSender) Send(ctx context.Context, cfg Config, _ Secrets, env Env, msg Message) error {
	s := env.SMTP
	if err := checkSMTP(s); err != nil {
		return err
	}
	from, _ := mail.ParseAddress(s.From)
	var rcpt []string
	for _, a := range cfg.To {
		addr, err := mail.ParseAddress(a)
		if err != nil {
			return invalid("recipient %q is not an e-mail address", a)
		}
		rcpt = append(rcpt, addr.Address)
	}
	data, err := buildMail(from, cfg.To, msg, cfg.Language, env.now())
	if err != nil {
		return err
	}
	timeout := env.SMTPTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := deliver(ctx, s, env.TLSConfig, from.Address, rcpt, data); err != nil {
		return fmt.Errorf("e-mail: %s", httpx.Scrub(err.Error(), s.Password))
	}
	return nil
}

func tlsConfig(base *tls.Config, host string) *tls.Config {
	if base != nil {
		c := base.Clone()
		if c.ServerName == "" {
			c.ServerName = host
		}
		return c
	}
	return &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
}

func deliver(ctx context.Context, s SMTP, tc *tls.Config, from string, rcpt []string, data []byte) error {
	addr := net.JoinHostPort(s.Host, strconv.Itoa(smtpPort(s)))
	nd := &net.Dialer{Timeout: 15 * time.Second}
	var (
		conn net.Conn
		err  error
	)
	if s.Security == "tls" {
		d := &tls.Dialer{NetDialer: nd, Config: tlsConfig(tc, s.Host)}
		conn, err = d.DialContext(ctx, "tcp", addr)
	} else {
		conn, err = nd.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer func() { _ = c.Close() }()
	if err := c.Hello("locarr"); err != nil {
		return err
	}
	if s.Security == "" || s.Security == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("the mail server does not offer STARTTLS")
		}
		if err := c.StartTLS(tlsConfig(tc, s.Host)); err != nil {
			return err
		}
	}
	if s.Username != "" {
		// PlainAuth refuses to send the password without TLS unless the server is on this machine.
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, r := range rcpt {
		if err := c.Rcpt(r); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func headerSafe(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("\r", " ", "\n", " ").Replace(s)), " ")
}

// buildMail renders a multipart/alternative message with a plain and an HTML part.
func buildMail(from *mail.Address, to []string, msg Message, l string, now time.Time) ([]byte, error) {
	var tos []string
	for _, a := range to {
		if strings.ContainsAny(a, "\r\n") {
			return nil, invalid("recipient %q is not an e-mail address", a)
		}
		addr, err := mail.ParseAddress(a)
		if err != nil {
			return nil, invalid("recipient %q is not an e-mail address", a)
		}
		tos = append(tos, addr.String())
	}
	id := make([]byte, 12)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	domain := "locarr"
	if _, d, ok := strings.Cut(from.Address, "@"); ok && d != "" {
		domain = d
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	var head bytes.Buffer
	for _, h := range [][2]string{{"From", from.String()}, {"To", strings.Join(tos, ", ")},
		{"Subject", mime.QEncoding.Encode("utf-8", headerSafe(msg.Headline))}, {"Date", now.Format(time.RFC1123Z)},
		{"Message-ID", "<" + hex.EncodeToString(id) + "@" + domain + ">"}, {"MIME-Version", "1.0"},
		{"Content-Type", "multipart/alternative; boundary=" + mw.Boundary()}, {"Auto-Submitted", "auto-generated"}} {
		head.WriteString(h[0] + ": " + h[1] + "\r\n")
	}
	head.WriteString("\r\n")

	plain := msg.Headline + "\n"
	if msg.Body != "" {
		plain += "\n" + msg.Body + "\n"
	}
	if msg.URL != "" {
		plain += "\n" + text(l, "open") + ": " + msg.URL + "\n"
	}
	var h strings.Builder
	h.WriteString(`<!doctype html><html><body style="font-family:sans-serif;color:#1f2328">`)
	if msg.PosterURL != "" {
		h.WriteString(`<img src="` + html.EscapeString(msg.PosterURL) + `" alt="" width="120" style="float:right;margin:0 0 12px 12px;border-radius:6px">`)
	}
	h.WriteString(`<h2 style="margin:0 0 12px">` + html.EscapeString(msg.Headline) + `</h2>`)
	for _, line := range strings.Split(msg.Body, "\n") {
		if strings.TrimSpace(line) != "" {
			h.WriteString(`<p style="margin:0 0 8px">` + html.EscapeString(line) + `</p>`)
		}
	}
	if msg.URL != "" {
		h.WriteString(`<p><a href="` + html.EscapeString(msg.URL) + `">` + html.EscapeString(text(l, "open")) + `</a></p>`)
	}
	h.WriteString(`</body></html>`)
	for _, part := range []struct{ typ, content string }{{"text/plain", plain}, {"text/html", h.String()}} {
		pw, err := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {part.typ + "; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"}})
		if err != nil {
			return nil, err
		}
		qw := quotedprintable.NewWriter(pw)
		if _, err := qw.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(part.content, "\r\n", "\n"), "\n", "\r\n"))); err != nil {
			return nil, err
		}
		if err := qw.Close(); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return append(head.Bytes(), body.Bytes()...), nil
}
