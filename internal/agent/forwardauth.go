package agent

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const desktopStartDebounce = 10 * time.Second

// desktopStart is the background desktop start state of one box; guarded by Agent.desktopStartMu.
type desktopStart struct {
	running bool
	last    time.Time
}

// startDesktopInBackground starts the box desktop off the request path. At most one start runs per box,
// and no new start is attempted within desktopStartDebounce of the last attempt.
// ponytail: desktopStarts keeps one entry per box ID seen on this node and is never pruned.
func (a *Agent) startDesktopInBackground(boxID string) {
	a.desktopStartMu.Lock()
	state := a.desktopStarts[boxID]
	if state == nil {
		state = &desktopStart{}
		a.desktopStarts[boxID] = state
	}
	if state.running || time.Since(state.last) < desktopStartDebounce {
		a.desktopStartMu.Unlock()
		return
	}
	state.running = true
	state.last = time.Now()
	a.desktopStartMu.Unlock()

	go func() {
		defer func() {
			a.desktopStartMu.Lock()
			state.running = false
			a.desktopStartMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := a.ensureDesktop(ctx, boxName(boxID)); err != nil {
			log.Printf("box desktop start box=%s error=%q", boxID, err)
		}
	}()
}

// ForwardAuthHandler returns the local-only Traefik forward-auth endpoint.
func (a *Agent) ForwardAuthHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/forward", a.forwardAuthRequest)
	return mux
}

func (a *Agent) forwardAuthRequest(w http.ResponseWriter, r *http.Request) {
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	if forwardedHost, _, err := net.SplitHostPort(host); err == nil {
		host = forwardedHost
	}
	host = strings.ToLower(host)
	forwardedURI := r.Header.Get("X-Forwarded-Uri")
	parsed, err := url.ParseRequestURI(forwardedURI)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	token := parsed.Query().Get("token")
	fromQuery := token != ""
	if !fromQuery {
		if cookie, err := r.Cookie("paddock_auth"); err == nil {
			token = cookie.Value
		}
	}
	a.mu.RLock()
	credential, ok := a.forwardAuth[host]
	a.mu.RUnlock()
	if !ok || token == "" || !tokenMatches(credential.hash, token) || (!credential.expiresAt.IsZero() && time.Now().After(credential.expiresAt)) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if fromQuery {
		cookie := &http.Cookie{Name: "paddock_auth", Value: token, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: 3600}
		if !credential.expiresAt.IsZero() {
			cookie.Expires = credential.expiresAt
			remaining := int(time.Until(credential.expiresAt).Seconds())
			if remaining < 1 {
				remaining = 1
			}
			cookie.MaxAge = remaining
		}
		http.SetCookie(w, cookie)
	}
	if credential.desktop {
		a.startDesktopInBackground(credential.boxID)
	}
	w.WriteHeader(http.StatusNoContent)
}

func tokenMatches(expectedHash, token string) bool {
	if expectedHash == "" || token == "" {
		return false
	}
	actual := sha256.Sum256([]byte(token))
	expected := []byte(strings.ToLower(expectedHash))
	actualHex := []byte(hex.EncodeToString(actual[:]))
	if len(expected) != len(actualHex) {
		return false
	}
	return subtle.ConstantTimeCompare(expected, actualHex) == 1
}
