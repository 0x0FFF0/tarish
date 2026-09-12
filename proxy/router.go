package proxy

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"net"
	"strings"
	"sync"
	"time"
)

type RoutePolicy struct {
	ProbeTimeout        time.Duration
	SelectBudget        time.Duration
	ProbeConcurrency    int
	CooldownStart       time.Duration
	CooldownCap         time.Duration
	DirectProbeInterval time.Duration
	RecoveryDirectMin   time.Duration
	RecoverySuccesses   int
}

func DefaultPolicy() RoutePolicy {
	return RoutePolicy{
		ProbeTimeout:        ProbeTimeout,
		SelectBudget:        SelectBudget,
		ProbeConcurrency:    ProbeConcurrency,
		CooldownStart:       CooldownStart,
		CooldownCap:         CooldownCap,
		DirectProbeInterval: DirectProbeEvery,
		RecoveryDirectMin:   RecoveryDirectMin,
		RecoverySuccesses:   RecoverySuccesses,
	}
}

type nodeState struct {
	spec          NodeSpec
	dialer        Dialer
	cooldownUntil time.Time
	cooldownFor   time.Duration
	lastErr       string
	consecutiveOK int
	lastOK        time.Time
}

type Router struct {
	mu            sync.Mutex
	policy        RoutePolicy
	clock         Clock
	jitter        func() float64
	target        Target
	pin           string
	tlsCfg        *tls.Config
	nodes         map[string]*nodeState
	order         []string
	lastSuccess   string
	state         RouteState
	directSince   time.Time
	enabled       bool
	newDialer     func(NodeSpec) (Dialer, error)
	directDial    func(ctx context.Context, addr string) (net.Conn, error)
	reconnect     func()
	targetTLSFail bool
	cacheExpired  bool
}

func NewRouter(target Target, pin string, policy RoutePolicy, clock Clock) *Router {
	if clock == nil {
		clock = realClock{}
	}
	if policy.ProbeConcurrency <= 0 {
		policy = DefaultPolicy()
	}
	return &Router{
		policy:     policy,
		clock:      clock,
		jitter:     jitterUnit,
		target:     target,
		pin:        strings.ToUpper(strings.TrimSpace(pin)),
		tlsCfg:     pinTLSConfig(pin, target.Host),
		nodes:      map[string]*nodeState{},
		state:      RouteDisabled,
		newDialer:  func(spec NodeSpec) (Dialer, error) { return NewAdapter(spec) },
		directDial: defaultDirectDial,
	}
}

func defaultDirectDial(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: ProbeTimeout}
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, Sanitize(err)
	}
	return c, nil
}

func pinTLSConfig(pin, serverName string) *tls.Config {
	pin = strings.ToUpper(strings.TrimSpace(pin))
	cfg := &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	}
	if ip := net.ParseIP(serverName); ip == nil {
		cfg.ServerName = serverName
	}
	cfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return errCode("tls_pin_mismatch")
		}
		sum := sha256.Sum256(rawCerts[0])
		got := strings.ToUpper(hex.EncodeToString(sum[:]))
		if pin != "" && got != pin {
			return errCode("tls_pin_mismatch")
		}
		return nil
	}
	return cfg
}

func (r *Router) SetReconnect(fn func()) {
	r.mu.Lock()
	r.reconnect = fn
	r.mu.Unlock()
}

func (r *Router) Enable(on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled = on
	if !on {
		r.state = RouteDisabled
	} else if r.state == RouteDisabled {
		r.state = RouteWaiting
	}
}

func (r *Router) SetCacheExpired(expired bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cacheExpired = expired
	if expired && r.enabled && r.state == RouteProxied {
		r.state = RouteDirectFallback
		if r.directSince.IsZero() {
			r.directSince = r.clock.Now()
		}
	}
}

func (r *Router) CacheExpired() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cacheExpired
}

func (r *Router) State() RouteState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

func (r *Router) ReplaceSnapshot(snap *Snapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if snap == nil {
		r.retireMissing(nil)
		return
	}
	incoming := map[string]NodeSpec{}
	var order []string
	for _, n := range snap.Nodes {
		incoming[n.ID] = n
		order = append(order, n.ID)
	}
	r.order = order
	r.retireMissing(incoming)
	for id, spec := range incoming {
		cur, ok := r.nodes[id]
		if !ok {
			d, err := r.newDialer(spec)
			if err != nil {
				continue
			}
			r.nodes[id] = &nodeState{spec: spec, dialer: d}
			continue
		}
		if !cur.spec.TransportEqual(spec) {
			_ = cur.dialer.Close()
			d, err := r.newDialer(spec)
			if err != nil {
				delete(r.nodes, id)
				continue
			}
			r.nodes[id] = &nodeState{spec: spec, dialer: d}
			if r.lastSuccess == id {
				r.lastSuccess = ""
			}
			continue
		}
		cur.spec.Name = spec.Name
	}
}

func (r *Router) retireMissing(incoming map[string]NodeSpec) {
	for id, st := range r.nodes {
		if incoming != nil {
			if _, ok := incoming[id]; ok {
				continue
			}
		}
		_ = st.dialer.Close()
		delete(r.nodes, id)
		if r.lastSuccess == id {
			r.lastSuccess = ""
		}
	}
}

func (r *Router) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, st := range r.nodes {
		_ = st.dialer.Close()
		delete(r.nodes, id)
	}
	r.lastSuccess = ""
}

func (r *Router) Dial(ctx context.Context, host string, port int) (net.Conn, error) {
	r.mu.Lock()
	enabled := r.enabled
	expired := r.cacheExpired
	last := r.lastSuccess
	r.mu.Unlock()
	if !enabled {
		return r.dialDirect(ctx)
	}
	if expired {
		r.setDirect()
		return r.dialDirect(ctx)
	}

	budget := r.policy.SelectBudget
	if deadline, ok := ctx.Deadline(); ok {
		if remain := time.Until(deadline); remain < budget {
			budget = remain
		}
	}
	selCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	if last != "" && r.eligible(last) {
		c, err := r.dialNode(selCtx, last, host, port)
		if err == nil {
			r.setProxied(last)
			return c, nil
		}
		r.failNode(last, err)
	}

	c, id, err := r.selectDial(selCtx, host, port)
	if err == nil {
		r.setProxied(id)
		return c, nil
	}
	r.setDirect()
	return r.dialDirect(ctx)
}

func (r *Router) dialDirect(ctx context.Context) (net.Conn, error) {
	return r.directDial(ctx, r.target.Addr())
}

func (r *Router) dialNode(ctx context.Context, id, host string, port int) (net.Conn, error) {
	r.mu.Lock()
	st := r.nodes[id]
	r.mu.Unlock()
	if st == nil {
		return nil, errCode("retired")
	}
	c, err := st.dialer.DialTCP(ctx, host, uint16(port))
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (r *Router) eligible(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.nodes[id]
	if !ok {
		return false
	}
	if st.cooldownUntil.After(r.clock.Now()) {
		return false
	}
	return true
}

func (r *Router) selectDial(ctx context.Context, host string, port int) (net.Conn, string, error) {
	ids := r.eligibleIDs()
	if len(ids) == 0 {
		return nil, "", errCode("no_eligible_node")
	}
	type result struct {
		id   string
		conn net.Conn
		err  error
	}
	ch := make(chan result, len(ids))
	sem := make(chan struct{}, r.policy.ProbeConcurrency)
	var wg sync.WaitGroup
	for _, id := range ids {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				ch <- result{id: id, err: ctx.Err()}
				return
			}
			pctx, cancel := context.WithTimeout(ctx, r.policy.ProbeTimeout)
			defer cancel()
			c, err := r.dialNode(pctx, id, host, port)
			if err != nil {
				r.failNode(id, err)
				ch <- result{id: id, err: err}
				return
			}
			ch <- result{id: id, conn: c}
		}()
	}
	go func() {
		wg.Wait()
		close(ch)
	}()
	var firstErr error
	var extras []net.Conn
	var winner result
	found := false
	for res := range ch {
		if res.conn != nil {
			if !found {
				winner = res
				found = true
			} else {
				extras = append(extras, res.conn)
			}
			continue
		}
		if firstErr == nil {
			firstErr = res.err
		}
	}
	for _, c := range extras {
		_ = c.Close()
	}
	if found {
		return winner.conn, winner.id, nil
	}
	if firstErr == nil {
		firstErr = errCode("no_eligible_node")
	}
	return nil, "", firstErr
}

func (r *Router) eligibleIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.clock.Now()
	var ids []string
	for _, id := range r.order {
		st, ok := r.nodes[id]
		if !ok {
			continue
		}
		if st.cooldownUntil.After(now) {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func (r *Router) failNode(id string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.nodes[id]
	if !ok {
		return
	}
	code := ErrorCode(err)
	st.lastErr = code
	st.consecutiveOK = 0
	if code == "tls_pin_mismatch" {
		return
	}
	next := st.cooldownFor
	if next <= 0 {
		next = r.policy.CooldownStart
	} else {
		next *= 2
		if next > r.policy.CooldownCap {
			next = r.policy.CooldownCap
		}
	}
	j := 1.0
	if r.jitter != nil {
		j = 0.85 + r.jitter()*0.3
	}
	d := time.Duration(float64(next) * j)
	st.cooldownFor = next
	st.cooldownUntil = r.clock.Now().Add(d)
}

func (r *Router) setProxied(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastSuccess = id
	r.state = RouteProxied
	r.directSince = time.Time{}
	r.targetTLSFail = false
	if st := r.nodes[id]; st != nil {
		st.cooldownFor = 0
		st.cooldownUntil = time.Time{}
		st.lastErr = ""
	}
}

func (r *Router) setDirect() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != RouteDirectFallback {
		r.directSince = r.clock.Now()
	}
	r.state = RouteDirectFallback
}

func (r *Router) ProbeOnce(ctx context.Context) map[string]string {
	ids := r.eligibleIDs()
	out := map[string]string{}
	var pinFails, otherFails, oks int
	for _, id := range ids {
		pctx, cancel := context.WithTimeout(ctx, r.policy.ProbeTimeout)
		err := r.probeNode(pctx, id)
		cancel()
		if err != nil {
			code := ErrorCode(err)
			out[id] = code
			r.failNode(id, err)
			if code == "tls_pin_mismatch" {
				pinFails++
			} else {
				otherFails++
			}
			continue
		}
		out[id] = "ok"
		oks++
		r.noteProbeOK(id)
	}
	if oks == 0 && pinFails > 0 && otherFails == 0 {
		r.mu.Lock()
		r.targetTLSFail = true
		for _, st := range r.nodes {
			if st.lastErr == "tls_pin_mismatch" {
				st.cooldownUntil = time.Time{}
				st.cooldownFor = 0
			}
		}
		r.mu.Unlock()
	}
	r.maybeRecover()
	return out
}

func (r *Router) probeNode(ctx context.Context, id string) error {
	host, port := r.target.Host, r.target.Port
	c, err := r.dialNode(ctx, id, host, port)
	if err != nil {
		return err
	}
	defer c.Close()
	tlsConn := tls.Client(c, r.tlsCfg)
	defer tlsConn.Close()
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		if strings.Contains(err.Error(), "tls_pin_mismatch") {
			return errCode("tls_pin_mismatch")
		}
		return Sanitize(err)
	}
	return nil
}

func (r *Router) ProbeDirect(ctx context.Context) error {
	c, err := r.dialDirect(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	tlsConn := tls.Client(c, r.tlsCfg)
	defer tlsConn.Close()
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		if strings.Contains(err.Error(), "tls_pin_mismatch") {
			return errCode("tls_pin_mismatch")
		}
		return Sanitize(err)
	}
	return nil
}

func (r *Router) noteProbeOK(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.nodes[id]
	if !ok {
		return
	}
	st.consecutiveOK++
	st.lastOK = r.clock.Now()
	st.lastErr = ""
	st.cooldownUntil = time.Time{}
	st.cooldownFor = 0
}

func (r *Router) maybeRecover() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != RouteDirectFallback {
		return
	}
	if r.directSince.IsZero() || r.clock.Now().Sub(r.directSince) < r.policy.RecoveryDirectMin {
		return
	}
	need := r.policy.RecoverySuccesses
	var winner string
	for _, id := range r.order {
		st := r.nodes[id]
		if st != nil && st.consecutiveOK >= need {
			winner = id
			break
		}
	}
	if winner == "" {
		return
	}
	r.lastSuccess = winner
	r.state = RouteProxied
	r.directSince = time.Time{}
	fn := r.reconnect
	if fn != nil {
		go fn()
	}
}

func (r *Router) Public(now time.Time, snap *Snapshot, refreshCode string) PublicStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := PublicStatus{
		ProxyEnabled: r.enabled,
		Configured:   snap != nil && len(snap.Nodes) > 0,
		Route:        r.state,
		ErrorCode:    refreshCode,
	}
	if !r.enabled {
		st.Route = RouteDisabled
	}
	if snap != nil {
		st.Nodes = len(snap.Nodes)
		st.Skipped = snap.Skipped
		stale, expired, age := CacheFreshness(snap.ValidatedAt, now)
		st.Stale = stale
		st.Expired = expired
		st.CacheAge = FormatAge(age)
		for _, n := range snap.Nodes {
			ns := NodeStatus{ID: n.ID, Health: "unknown"}
			if rs, ok := r.nodes[n.ID]; ok {
				ns.LastErrorCode = rs.lastErr
				if rs.cooldownUntil.After(now) {
					ns.Health = "cooldown"
					ns.CooldownUntil = rs.cooldownUntil.UTC().Format(time.RFC3339)
				} else if rs.lastErr != "" {
					ns.Health = "failed"
				} else if r.lastSuccess == n.ID && r.state == RouteProxied {
					ns.Health = "ok"
				} else {
					ns.Health = "idle"
				}
			}
			st.NodeStatuses = append(st.NodeStatuses, ns)
		}
	}
	return st
}

func SHA256Hex(cert []byte) string {
	sum := sha256.Sum256(cert)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}
