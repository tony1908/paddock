package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

var (
	errDesktopNotRunning = errors.New("box desktop session is not running")
	errDesktopDisabled   = errors.New("box desktop is disabled")
)

const (
	desktopStartCommand      = "/usr/local/bin/paddock-desktop"
	desktopStartPollInterval = 500 * time.Millisecond
	desktopStartTimeout      = 20 * time.Second
)

// desktopEnvScript prints the environ of the box desktop session. It takes the oldest xfce4-session of abc,
// else the selkies process, else the first process whose environment has DISPLAY=:1. It exits non-zero if none.
// ponytail: /proc scan is the fallback only; the pgrep lookups cover the stock webtop image.
const desktopEnvScript = `pid=$(pgrep -u abc -o xfce4-session || pgrep -u abc -o -f selkies); ` +
	`if [ -z "$pid" ]; then for f in /proc/[0-9]*/environ; do ` +
	`if tr '\0' '\n' < "$f" 2>/dev/null | grep -qx 'DISPLAY=:1'; then pid=${f#/proc/}; pid=${pid%/environ}; break; fi; done; fi; ` +
	`[ -n "$pid" ] && cat "/proc/$pid/environ"`

// desktopEnv returns DISPLAY and the session bus variables of the box desktop session.
func (a *Agent) desktopEnv(ctx context.Context, container string) (map[string]string, error) {
	var environ bytes.Buffer
	code, err := a.docker.Exec(ctx, container, "root", "", nil, []string{"sh", "-c", desktopEnvScript}, nil, &environ, io.Discard)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, errDesktopNotRunning
	}
	env, ok := desktopEnvFromEnviron(environ.Bytes())
	if !ok {
		return nil, errDesktopNotRunning
	}
	return env, nil
}

// ensureDesktop starts the desktop with paddock-desktop (idempotent, exits 0 once xfce4-session is up), then returns its env.
// Exit 3 from paddock-desktop means the box is headless. Images without paddock-desktop exit 126/127 and fall back to desktopEnv alone.
func (a *Agent) ensureDesktop(ctx context.Context, container string) (map[string]string, error) {
	code, err := a.docker.Exec(ctx, container, "root", "", nil, []string{desktopStartCommand, "start"}, nil, io.Discard, io.Discard)
	if err != nil {
		return nil, err
	}
	switch code {
	case 0:
	case 3:
		return nil, errDesktopDisabled
	case 126, 127:
		return a.desktopEnv(ctx, container)
	default:
		return nil, errDesktopNotRunning
	}
	env, err := a.desktopEnv(ctx, container)
	if !errors.Is(err, errDesktopNotRunning) {
		return env, err
	}
	// ponytail: fixed-interval poll until the session appears; event-driven readiness if this proves slow.
	ctx, cancel := context.WithTimeout(ctx, desktopStartTimeout)
	defer cancel()
	ticker := time.NewTicker(desktopStartPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, errDesktopNotRunning
		case <-ticker.C:
		}
		env, err := a.desktopEnv(ctx, container)
		if !errors.Is(err, errDesktopNotRunning) {
			return env, err
		}
	}
}

// desktopEnvFromEnviron extracts the display variables from a NUL-separated environ dump.
// ok is false unless the process runs on DISPLAY=:1.
func desktopEnvFromEnviron(environ []byte) (map[string]string, bool) {
	vars := map[string]string{}
	for _, entry := range strings.Split(string(environ), "\x00") {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		switch key {
		case "DISPLAY", "DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR":
			vars[key] = value
		}
	}
	if vars["DISPLAY"] != ":1" {
		return nil, false
	}
	return vars, true
}
