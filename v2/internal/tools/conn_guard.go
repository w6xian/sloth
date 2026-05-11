package tools

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

var ErrConnBanned = errors.New("connection banned")
var ErrConnLimited = errors.New("connection limited")

type GuardReject struct {
	Err    error
	Kind   string
	Reason string
}

func (e *GuardReject) Error() string {
	if e == nil {
		return ""
	}
	if e.Reason != "" {
		return e.Reason
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return "rejected"
}

type ConnLimits struct {
	MaxGlobal int64
	MaxPerIP  int64
	MaxWS     int64
	MaxTCP    int64
	MaxKCP    int64

	AutoBanEnabled   bool
	AutoBanWindow    time.Duration
	AutoBanThreshold int64
	AutoBanTTL       time.Duration
}

type banEntry struct {
	ExpireAt time.Time
	Reason   string
}

type ipEntry struct {
	Count    int64
	LastSeen time.Time
}

type ipAttemptEntry struct {
	Count      int64
	WindowFrom time.Time
	LastSeen   time.Time
}

type ConnGuard struct {
	mu     sync.Mutex
	limits ConnLimits

	global int64
	ws     int64
	tcp    int64
	kcp    int64

	perIP  map[string]*ipEntry
	banned map[string]banEntry
	attempts map[string]*ipAttemptEntry
}

func NewConnGuard(limits ConnLimits) *ConnGuard {
	return &ConnGuard{
		limits: limits,
		perIP:  map[string]*ipEntry{},
		banned: map[string]banEntry{},
		attempts: map[string]*ipAttemptEntry{},
	}
}

func (g *ConnGuard) SetLimits(limits ConnLimits) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.limits = limits
}

func (g *ConnGuard) BanIP(ip string, ttl time.Duration, reason string) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	expireAt := time.Time{}
	if ttl > 0 {
		expireAt = time.Now().Add(ttl)
	}
	g.banned[ip] = banEntry{ExpireAt: expireAt, Reason: reason}
}

func (g *ConnGuard) UnbanIP(ip string) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.banned, ip)
}

func (g *ConnGuard) IsBannedIP(ip string) (bool, string) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return false, ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	entry, ok := g.banned[ip]
	if !ok {
		return false, ""
	}
	if !entry.ExpireAt.IsZero() && time.Now().After(entry.ExpireAt) {
		delete(g.banned, ip)
		return false, ""
	}
	return true, entry.Reason
}

func (g *ConnGuard) Acquire(protocol string, ip string) (func(), error) {
	ip = strings.TrimSpace(ip)
	g.mu.Lock()
	defer g.mu.Unlock()

	if ip != "" {
		if banned, reason := g.isBannedLocked(ip); banned {
			return nil, &GuardReject{Err: ErrConnBanned, Kind: "banned_ip", Reason: reason}
		}
	}

	if g.limits.MaxGlobal > 0 && g.global >= g.limits.MaxGlobal {
		if ip != "" && g.autoBanOnRejectLocked(ip, "limit_global") {
			return nil, &GuardReject{Err: ErrConnBanned, Kind: "banned_ip_auto", Reason: "auto banned"}
		}
		return nil, &GuardReject{Err: ErrConnLimited, Kind: "limit_global", Reason: "too many connections"}
	}

	switch protocol {
	case "ws":
		if g.limits.MaxWS > 0 && g.ws >= g.limits.MaxWS {
			if ip != "" && g.autoBanOnRejectLocked(ip, "limit_ws") {
				return nil, &GuardReject{Err: ErrConnBanned, Kind: "banned_ip_auto", Reason: "auto banned"}
			}
			return nil, &GuardReject{Err: ErrConnLimited, Kind: "limit_ws", Reason: "too many websocket connections"}
		}
	case "tcp":
		if g.limits.MaxTCP > 0 && g.tcp >= g.limits.MaxTCP {
			if ip != "" && g.autoBanOnRejectLocked(ip, "limit_tcp") {
				return nil, &GuardReject{Err: ErrConnBanned, Kind: "banned_ip_auto", Reason: "auto banned"}
			}
			return nil, &GuardReject{Err: ErrConnLimited, Kind: "limit_tcp", Reason: "too many tcp connections"}
		}
	case "kcp":
		if g.limits.MaxKCP > 0 && g.kcp >= g.limits.MaxKCP {
			if ip != "" && g.autoBanOnRejectLocked(ip, "limit_kcp") {
				return nil, &GuardReject{Err: ErrConnBanned, Kind: "banned_ip_auto", Reason: "auto banned"}
			}
			return nil, &GuardReject{Err: ErrConnLimited, Kind: "limit_kcp", Reason: "too many kcp connections"}
		}
	}

	if ip != "" && g.limits.MaxPerIP > 0 {
		e := g.perIP[ip]
		if e == nil {
			e = &ipEntry{}
			g.perIP[ip] = e
		}
		if e.Count >= g.limits.MaxPerIP {
			if g.autoBanOnRejectLocked(ip, "limit_ip") {
				return nil, &GuardReject{Err: ErrConnBanned, Kind: "banned_ip_auto", Reason: "auto banned"}
			}
			return nil, &GuardReject{Err: ErrConnLimited, Kind: "limit_ip", Reason: "too many connections from ip"}
		}
	}

	g.global++
	switch protocol {
	case "ws":
		g.ws++
	case "tcp":
		g.tcp++
	case "kcp":
		g.kcp++
	}
	if ip != "" {
		e := g.perIP[ip]
		if e == nil {
			e = &ipEntry{}
			g.perIP[ip] = e
		}
		e.Count++
		e.LastSeen = time.Now()
		delete(g.attempts, ip)
	}
	g.cleanupLocked()

	var once sync.Once
	release := func() {
		once.Do(func() {
			g.release(protocol, ip)
		})
	}
	return release, nil
}

func (g *ConnGuard) RecordReject(ip string, reason string) (banned bool) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if bannedNow, _ := g.isBannedLocked(ip); bannedNow {
		return true
	}
	return g.autoBanOnRejectLocked(ip, reason)
}

func (g *ConnGuard) release(protocol string, ip string) {
	ip = strings.TrimSpace(ip)
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.global > 0 {
		g.global--
	}
	switch protocol {
	case "ws":
		if g.ws > 0 {
			g.ws--
		}
	case "tcp":
		if g.tcp > 0 {
			g.tcp--
		}
	case "kcp":
		if g.kcp > 0 {
			g.kcp--
		}
	}

	if ip == "" {
		return
	}
	e := g.perIP[ip]
	if e == nil {
		return
	}
	if e.Count > 0 {
		e.Count--
	}
	if e.Count == 0 && time.Since(e.LastSeen) > 2*time.Minute {
		delete(g.perIP, ip)
	}
	g.cleanupLocked()
}

func (g *ConnGuard) autoBanOnRejectLocked(ip string, reason string) (banned bool) {
	if !g.limits.AutoBanEnabled || g.limits.AutoBanThreshold <= 0 {
		return false
	}
	window := g.limits.AutoBanWindow
	if window <= 0 {
		window = 30 * time.Second
	}
	now := time.Now()
	a := g.attempts[ip]
	if a == nil {
		a = &ipAttemptEntry{WindowFrom: now, LastSeen: now, Count: 1}
		g.attempts[ip] = a
	} else {
		if now.Sub(a.WindowFrom) > window {
			a.WindowFrom = now
			a.Count = 1
		} else {
			a.Count++
		}
		a.LastSeen = now
	}
	if a.Count < g.limits.AutoBanThreshold {
		return false
	}
	ttl := g.limits.AutoBanTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	msg := "auto ban: too many rejects"
	if strings.TrimSpace(reason) != "" {
		msg = "auto ban: " + strings.TrimSpace(reason)
	}
	g.banned[ip] = banEntry{ExpireAt: now.Add(ttl), Reason: msg}
	delete(g.attempts, ip)
	return true
}

func (g *ConnGuard) cleanupLocked() {
	now := time.Now()
	for ip, entry := range g.attempts {
		if now.Sub(entry.LastSeen) > 5*time.Minute {
			delete(g.attempts, ip)
		}
	}
	for ip, entry := range g.banned {
		if !entry.ExpireAt.IsZero() && now.After(entry.ExpireAt) {
			delete(g.banned, ip)
		}
	}
}

func (g *ConnGuard) isBannedLocked(ip string) (bool, string) {
	entry, ok := g.banned[ip]
	if !ok {
		return false, ""
	}
	if !entry.ExpireAt.IsZero() && time.Now().After(entry.ExpireAt) {
		delete(g.banned, ip)
		return false, ""
	}
	return true, entry.Reason
}

func RemoteIPFromAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return strings.TrimSpace(host)
	}
	return addr
}

func RemoteIPFromRequest(r *http.Request, trustProxyHeaders bool) string {
	if r == nil {
		return ""
	}
	if trustProxyHeaders {
		xff := r.Header.Get("X-Forwarded-For")
		if xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				ip := strings.TrimSpace(parts[0])
				if ip != "" {
					return ip
				}
			}
		}
	}
	return RemoteIPFromAddr(r.RemoteAddr)
}
