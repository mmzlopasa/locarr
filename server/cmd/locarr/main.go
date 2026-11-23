// Command locarr is the Locarr server.
//
//	locarr serve                 run the server (default)
//	locarr migrate               apply database migrations and exit
//	locarr healthcheck           exit 0 when the local server is ready (container HEALTHCHECK)
//	locarr version               print the version
//	locarr reset-admin [user]    give an administrator a new random password,
//	                             clear its lockout and two-factor, and sign it out
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/app"
	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/config"
	"github.com/mmzlopasa/locarr/server/internal/logging"
	"github.com/mmzlopasa/locarr/server/internal/version"
)

const usage = "usage: locarr [serve|migrate|healthcheck|version|reset-admin [username]]"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Stdout)
	stop()
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "version", "--version", "-v":
		_, err := fmt.Fprintf(out, "locarr %s (%s)\n", version.Version, version.Commit)
		return err
	case "healthcheck":
		return healthcheck(ctx)
	case "help", "--help", "-h":
		_, err := fmt.Fprintln(out, usage)
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cmd == "serve" {
		// The server also writes rotated files to /config/logs and keeps the
		// latest records for the in-app log viewer.
		log, sinks := logging.Setup(os.Stdout, cfg.LogLevel, filepath.Join(cfg.ConfigDir, "logs"))
		defer func() { _ = sinks.Close() }()
		return app.ServeWith(ctx, cfg, log, sinks)
	}
	switch cmd {
	case "migrate":
		d, _, err := app.OpenStorage(ctx, cfg)
		if err != nil {
			return err
		}
		defer func() { _ = d.Close() }()
		v, err := d.Version(ctx)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "database at schema version %d\n", v)
		return err
	case "reset-admin":
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		return resetAdmin(ctx, cfg, name, out)
	default:
		return fmt.Errorf("unknown command %q\n%s", cmd, usage)
	}
}

func resetAdmin(ctx context.Context, cfg config.Config, username string, out io.Writer) error {
	d, box, err := app.OpenStorage(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	user, password, err := auth.NewService(d, box, "").ResetAdmin(ctx, username)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Administrator %q can now sign in with this password:\n\n    %s\n\n"+
		"Two-factor authentication was turned off and all its sessions were signed out.\n"+
		"Change the password under Account after signing in.\n", user, password)
	return err
}

// healthcheck asks the local server for /readyz.
func healthcheck(ctx context.Context) error {
	addr := os.Getenv("LOCARR_HTTP_ADDR")
	if addr == "" {
		addr = ":8484"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// The address is this server's own listen address from its environment.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+"/readyz", nil) // #nosec G704
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req) // #nosec G704 -- local readiness probe
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("not ready: HTTP %d", res.StatusCode)
	}
	return nil
}
