package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

// Pool owns one Chromium process and hands out a limited number of tabs. The
// process starts on the first solve, stops after it has been idle for a while
// and is restarted after a crash, after many solves or when the container
// runs short of memory.
type Pool struct {
	Exec    string        // Chromium binary
	Tabs    int           // tabs at once
	Idle    time.Duration // stop the browser after this long without solves
	Sandbox string        // auto, on or off
	Log     *slog.Logger

	slots chan struct{}

	mu       sync.Mutex
	alloc    context.Context
	stopAll  context.CancelFunc
	bctx     context.Context // the browser itself
	inUse    int
	solves   int // since the last start
	version  string
	ua       string
	sandbox  bool
	restarts int
	lastUsed time.Time
	retire   bool // restart once the tabs in use are done
	timer    *time.Timer
}

// maxSolvesPerProcess restarts Chromium after this many solves, so slow leaks
// never pile up.
const maxSolvesPerProcess = 200

func (p *Pool) init() {
	if p.slots == nil {
		if p.Tabs <= 0 {
			p.Tabs = 2
		}
		p.slots = make(chan struct{}, p.Tabs)
	}
}

func (p *Pool) flags(sandbox bool, dataDir string) []chromedp.ExecAllocatorOption {
	opts := []chromedp.ExecAllocatorOption{
		chromedp.ExecPath(p.Exec),
		chromedp.UserDataDir(dataDir),
		chromedp.Flag("headless", "new"),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-component-update", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("disable-default-apps", true),
		chromedp.Flag("disable-breakpad", true),
		chromedp.Flag("disable-domain-reliability", true),
		chromedp.Flag("metrics-recording-only", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("no-pings", true),
		chromedp.Flag("password-store", "basic"),
		chromedp.Flag("use-mock-keychain", true),
		// Every request goes through the solve's guard proxy: no QUIC, no
		// direct WebRTC UDP, no proxy bypass for loopback.
		chromedp.Flag("disable-quic", true),
		chromedp.Flag("force-webrtc-ip-handling-policy", "disable_non_proxied_udp"),
		chromedp.Flag("proxy-server", "http://127.0.0.1:9"), // a dead end until a context sets its own
		chromedp.Flag("proxy-bypass-list", "<-loopback>"),
		// Behave like the browser a person runs rather than a test harness.
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("renderer-process-limit", strconv.Itoa(p.Tabs+1)),
		chromedp.Flag("js-flags", "--max-old-space-size=256"),
		chromedp.WindowSize(1366, 768),
	}
	if !sandbox {
		opts = append(opts, chromedp.NoSandbox)
	}
	return opts
}

// start launches Chromium (p.mu held).
func (p *Pool) start() error {
	dir, err := os.MkdirTemp("", "chromium-")
	if err != nil {
		return err
	}
	try := func(sandbox bool) error {
		alloc, cancel := chromedp.NewExecAllocator(context.Background(), p.flags(sandbox, dir)...)
		bctx, bcancel := chromedp.NewContext(alloc)
		// The first call starts the browser, which then lives as long as its
		// context: no timeout on it, a timer stops a start that hangs.
		hang := time.AfterFunc(30*time.Second, bcancel)
		var v browser.GetVersionResult
		err := chromedp.Do(bctx)
		if err == nil {
			v, err = cdp.Call(bctx, chromedp.FromContext(bctx).Browser, browser.GetVersion, cdp.Empty{})
		}
		hang.Stop()
		prod, ua := v.Product, v.UserAgent
		if err != nil {
			bcancel()
			cancel()
			return err
		}
		p.alloc, p.bctx = alloc, bctx
		p.stopAll = func() { bcancel(); cancel(); _ = os.RemoveAll(dir) }
		p.version, p.sandbox = prod, sandbox
		p.ua = strings.Replace(ua, "HeadlessChrome/", "Chrome/", 1)
		p.solves = 0
		return nil
	}
	switch p.Sandbox {
	case "on":
		err = try(true)
	case "off":
		err = try(false)
	default:
		if err = try(true); err != nil {
			p.Log.Warn("Chromium's sandbox is not available in this container; running without it", "error", err)
			err = try(false)
		}
	}
	if err != nil {
		_ = os.RemoveAll(dir)
		return err
	}
	p.Log.Info("browser started", "version", p.version, "sandbox", p.sandbox)
	return nil
}

// stop ends Chromium (p.mu held).
func (p *Pool) stop() {
	if p.stopAll != nil {
		p.stopAll()
		p.stopAll, p.bctx, p.alloc = nil, nil, nil
	}
}

// Close stops the browser.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.timer != nil {
		p.timer.Stop()
	}
	p.stop()
}

// ErrBusy is returned when every tab stays busy for the whole wait.
var ErrBusy = errors.New("every browser tab is busy")

// Acquire waits for a free tab and returns the running browser's context.
// release must be called when the tab is done.
func (p *Pool) Acquire(ctx context.Context) (bctx context.Context, ua, version string, release func(), err error) {
	p.init()
	select {
	case p.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, "", "", nil, ErrBusy
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bctx != nil && p.bctx.Err() != nil { // crashed
		p.Log.Warn("browser exited; starting it again")
		p.stop()
		p.restarts++
	}
	if p.bctx == nil {
		if err := p.start(); err != nil {
			<-p.slots
			return nil, "", "", nil, err
		}
	}
	p.inUse++
	p.solves++
	bctx = p.bctx
	once := sync.Once{}
	release = func() {
		once.Do(func() {
			p.mu.Lock()
			p.inUse--
			p.lastUsed = time.Now()
			if p.solves >= maxSolvesPerProcess || memoryTight() {
				p.retire = true
			}
			if p.retire && p.inUse == 0 {
				p.Log.Info("restarting the browser", "solves", p.solves)
				p.stop()
				p.restarts++
				p.retire = false
			}
			p.scheduleIdle()
			p.mu.Unlock()
			<-p.slots
		})
	}
	return bctx, p.ua, strings.TrimPrefix(p.version, "Chrome/"), release, nil
}

// scheduleIdle stops the browser once it has been idle long enough (p.mu held).
func (p *Pool) scheduleIdle() {
	if p.Idle <= 0 {
		return
	}
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(p.Idle, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.inUse == 0 && p.bctx != nil && time.Since(p.lastUsed) >= p.Idle {
			p.Log.Info("browser idle; stopping it")
			p.stop()
		}
	})
}

// PoolStatus is what the status endpoint reports.
type PoolStatus struct {
	Running  bool
	Version  string
	Sandbox  bool
	InUse    int
	Tabs     int
	Restarts int
}

// Status reports the pool.
func (p *Pool) Status() PoolStatus {
	p.init()
	p.mu.Lock()
	defer p.mu.Unlock()
	return PoolStatus{Running: p.bctx != nil && p.bctx.Err() == nil, Version: p.version, Sandbox: p.sandbox,
		InUse: p.inUse, Tabs: p.Tabs, Restarts: p.restarts}
}

// memoryTight reports whether the container uses more than 80% of its memory limit.
func memoryTight() bool {
	read := func(name string) (int64, bool) {
		b, err := os.ReadFile("/sys/fs/cgroup/" + name) // #nosec G304 -- fixed cgroup files
		if err != nil {
			return 0, false
		}
		v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		return v, err == nil
	}
	cur, ok1 := read("memory.current")
	limit, ok2 := read("memory.max") // "max" (no limit) does not parse
	return ok1 && ok2 && limit > 0 && cur*5 > limit*4
}
