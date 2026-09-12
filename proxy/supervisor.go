package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"tarish/antisleep"
	"tarish/config"
	"tarish/cpu"
	"tarish/xmrig"
)

type ctlReq struct {
	Op string `json:"op"`
}

type ctlResp struct {
	OK     bool          `json:"ok"`
	Ready  bool          `json:"ready"`
	Error  string        `json:"error,omitempty"`
	Status *PublicStatus `json:"status,omitempty"`
}

type Daemon struct {
	store        *Store
	router       *Router
	socks        *SOCKSServer
	lockFile     *os.File
	ctlLn        net.Listener
	minerPID     int
	ready        bool
	mu           sync.Mutex
	cancel       context.CancelFunc
	runCtx       context.Context
	loopCancel   context.CancelFunc
	proxyMu      sync.Mutex
	refreshErr   string
	reconnectN   int
	shutdownOnce sync.Once

	StartMiner func(binary, cfg string) (int, error)
	StopMiner  func(pid int) error
	FindBinary func() (string, error)
}

func NewDaemon() *Daemon {
	return &Daemon{
		StartMiner: xmrig.StartOwned,
		StopMiner:  xmrig.StopOwned,
		FindBinary: func() (string, error) {
			info, err := xmrig.GetInstalledBinaryPath()
			if err != nil {
				return "", err
			}
			return info.Path, nil
		},
	}
}

func RunDaemon() {
	d := NewDaemon()
	if err := d.Run(); err != nil {
		fmt.Printf("miner daemon: %v\n", ErrorCode(err))
		os.Exit(1)
	}
}

func (d *Daemon) Run() error {
	store, err := OpenStore()
	if err != nil {
		return err
	}
	d.store = store
	lock, err := acquireLock(store.LockPath())
	if err != nil {
		return err
	}
	d.lockFile = lock
	defer lock.Close()

	_ = writePID(store.SupervisorPIDPath(), os.Getpid())
	defer os.Remove(store.SupervisorPIDPath())

	reconcileLegacyMiner()

	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	d.runCtx = ctx
	defer cancel()

	if err := d.boot(ctx); err != nil {
		return err
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		d.Shutdown()
	}()

	return d.serveControl(ctx)
}

func (d *Daemon) boot(ctx context.Context) error {
	d.reconcileOwnedMinerPID()

	proxyOn := config.IsProxyEnabled()
	var socksAddr string
	if proxyOn {
		if err := d.startProxy(ctx); err != nil {
			return err
		}
		d.startProxyLoops(ctx)
		if d.socks != nil {
			socksAddr = d.socks.Addr()
		}
	}

	cpuInfo, err := cpu.Detect()
	if err != nil {
		return errCode("cpu_detect")
	}
	configPath, err := xmrig.SelectConfig(cpuInfo, xmrig.GetInstalledConfigPath())
	if err != nil {
		return errCode("config_select")
	}
	runtimePath, err := xmrig.PrepareManagedRuntime(configPath, cpuInfo, xmrig.ManagedOptions{
		ProxyEnabled: proxyOn,
		SOCKSAddr:    socksAddr,
		RuntimeDir:   d.store.RuntimeDir(),
		TokenPath:    d.store.TokenPath(),
	})
	if err != nil {
		return err
	}

	if d.StartMiner != nil {
		bin, err := d.FindBinary()
		if err != nil {
			return errCode("miner_binary")
		}
		pid, err := d.StartMiner(bin, runtimePath)
		if err != nil {
			return errCode("miner_start")
		}
		d.minerPID = pid
		_ = writePID(d.store.MinerPIDPath(), pid)
	}
	if err := antisleep.Enable(); err != nil {
		fmt.Printf("Warning: sleep prevention: %s\n", ErrorCode(err))
	}
	d.mu.Lock()
	d.ready = true
	d.mu.Unlock()
	return nil
}

func (d *Daemon) startProxy(ctx context.Context) error {
	if d.socks != nil {
		return nil
	}
	host, port, err := SplitHostPort(xmrig.TLSPoolURL)
	if err != nil {
		return err
	}
	d.router = NewRouter(Target{Host: host, Port: port}, xmrig.TLSFingerprint, DefaultPolicy(), realClock{})
	d.router.Enable(true)
	d.router.SetReconnect(func() { d.requestReconnect() })
	d.applyActiveSnapshot()

	target := Target{Host: host, Port: port}
	d.socks = &SOCKSServer{
		Target: target,
		Dial: func(h string, p int) (net.Conn, error) {
			dctx, cancel := context.WithTimeout(context.Background(), SelectBudget)
			defer cancel()
			return d.router.Dial(dctx, h, p)
		},
	}
	if _, err := d.socks.Listen("127.0.0.1:0"); err != nil {
		d.socks = nil
		return err
	}
	go d.socks.Serve()
	return nil
}

func (d *Daemon) startProxyLoops(parent context.Context) {
	d.mu.Lock()
	if d.loopCancel != nil {
		d.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	d.loopCancel = cancel
	d.mu.Unlock()
	go d.refreshLoop(ctx)
	go d.probeLoop(ctx)
}

func (d *Daemon) stopProxyLoops() {
	d.mu.Lock()
	cancel := d.loopCancel
	d.loopCancel = nil
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (d *Daemon) stopProxyStack() {
	d.stopProxyLoops()
	if d.socks != nil {
		_ = d.socks.Close()
		d.socks = nil
	}
	if d.router != nil {
		d.router.Close()
		d.router = nil
	}
}

func (d *Daemon) enableProxy() error {
	d.proxyMu.Lock()
	defer d.proxyMu.Unlock()
	ctx := d.runCtx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := d.startProxy(ctx); err != nil {
		return err
	}
	if d.socks == nil {
		return errCode("listen_failed")
	}
	d.startProxyLoops(ctx)
	return d.restartMiner()
}

func (d *Daemon) disableProxy() error {
	d.proxyMu.Lock()
	defer d.proxyMu.Unlock()
	d.stopProxyStack()
	return d.restartMiner()
}

func (d *Daemon) reconcileOwnedMinerPID() {
	if d.store == nil {
		return
	}
	pid, running := readPIDFile(d.store.MinerPIDPath())
	if running {
		if d.StopMiner != nil {
			_ = d.StopMiner(pid)
		} else {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	_ = os.Remove(d.store.MinerPIDPath())
}

func (d *Daemon) requestReconnect() {
	d.mu.Lock()
	d.reconnectN++
	d.mu.Unlock()
	d.proxyMu.Lock()
	_ = d.restartMiner()
	d.proxyMu.Unlock()
}

func (d *Daemon) restartMiner() error {
	if d.minerPID > 0 && d.StopMiner != nil {
		_ = d.StopMiner(d.minerPID)
		d.minerPID = 0
	}
	cpuInfo, err := cpu.Detect()
	if err != nil {
		return err
	}
	configPath, err := xmrig.SelectConfig(cpuInfo, xmrig.GetInstalledConfigPath())
	if err != nil {
		return err
	}
	proxyOn := config.IsProxyEnabled()
	socksAddr := ""
	if d.socks != nil {
		socksAddr = d.socks.Addr()
	}
	if proxyOn && socksAddr == "" {
		return errCode("listen_failed")
	}
	runtimePath, err := xmrig.PrepareManagedRuntime(configPath, cpuInfo, xmrig.ManagedOptions{
		ProxyEnabled: proxyOn,
		SOCKSAddr:    socksAddr,
		RuntimeDir:   d.store.RuntimeDir(),
		TokenPath:    d.store.TokenPath(),
	})
	if err != nil {
		return err
	}
	bin, err := d.FindBinary()
	if err != nil {
		return err
	}
	pid, err := d.StartMiner(bin, runtimePath)
	if err != nil {
		return err
	}
	d.minerPID = pid
	_ = writePID(d.store.MinerPIDPath(), pid)
	return nil
}

func (d *Daemon) refreshLoop(ctx context.Context) {
	f := NewFetcher(d.store, Format(config.GetProxyFormat()))
	timer := time.NewTimer(f.JitteredInterval())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if d.store != nil && d.store.ManagedActive() {
				d.applyActiveSnapshot()
				timer.Reset(f.JitteredInterval())
				continue
			}
			snap, err := f.Refresh(ctx)
			d.applyRefreshResult(snap, err)
			timer.Reset(f.JitteredInterval())
		}
	}
}

func (d *Daemon) probeLoop(ctx context.Context) {
	t := time.NewTicker(DirectProbeEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if d.router == nil {
				continue
			}
			if d.router.State() == RouteDirectFallback || d.router.State() == RouteWaiting {
				pctx, cancel := context.WithTimeout(ctx, SelectBudget)
				d.router.ProbeOnce(pctx)
				cancel()
			}
		}
	}
}

func (d *Daemon) serveControl(ctx context.Context) error {
	path := d.store.ControlSocketPath()
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return errCode("control_listen")
	}
	_ = os.Chmod(path, privateFilePerm)
	d.ctlLn = ln
	defer ln.Close()
	defer os.Remove(path)

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				if d.ctlLn == nil {
					return nil
				}
				return nil
			}
		}
		go d.handleCtl(conn)
	}
}

func (d *Daemon) handleCtl(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	dec := json.NewDecoder(bufio.NewReader(conn))
	var req ctlReq
	if err := dec.Decode(&req); err != nil {
		return
	}
	resp := d.dispatch(req)
	enc := json.NewEncoder(conn)
	_ = enc.Encode(resp)
}

func (d *Daemon) dispatch(req ctlReq) ctlResp {
	switch strings.ToLower(req.Op) {
	case "ready", "ping", "status":
		st := d.publicStatus()
		d.mu.Lock()
		ready := d.ready
		d.mu.Unlock()
		return ctlResp{OK: true, Ready: ready, Status: &st}
	case "reload-managed":
		d.applyActiveSnapshot()
		st := d.publicStatus()
		return ctlResp{OK: true, Ready: true, Status: &st}
	case "reload", "refresh":
		if d.store != nil && d.store.ManagedActive() {
			d.applyActiveSnapshot()
			st := d.publicStatus()
			return ctlResp{OK: true, Ready: true, Status: &st}
		}
		f := NewFetcher(d.store, Format(config.GetProxyFormat()))
		snap, err := f.Refresh(context.Background())
		if err != nil && snap == nil {
			return ctlResp{OK: false, Error: ErrorCode(err)}
		}
		d.applyRefreshResult(snap, err)
		st := d.publicStatus()
		return ctlResp{OK: true, Ready: true, Status: &st, Error: ErrorCode(err)}
	case "test":
		if d.router == nil {
			return ctlResp{OK: false, Error: "disabled"}
		}
		ctx, cancel := context.WithTimeout(context.Background(), SelectBudget)
		defer cancel()
		_ = d.router.ProbeOnce(ctx)
		st := d.publicStatus()
		return ctlResp{OK: true, Ready: true, Status: &st}
	case "enable":
		if err := d.enableProxy(); err != nil {
			return ctlResp{OK: false, Error: ErrorCode(err)}
		}
		st := d.publicStatus()
		return ctlResp{OK: true, Ready: true, Status: &st}
	case "disable":
		if err := d.disableProxy(); err != nil {
			return ctlResp{OK: false, Error: ErrorCode(err)}
		}
		st := d.publicStatus()
		return ctlResp{OK: true, Ready: true, Status: &st}
	case "reconnect":
		d.proxyMu.Lock()
		err := d.restartMiner()
		d.proxyMu.Unlock()
		if err != nil {
			return ctlResp{OK: false, Error: ErrorCode(err)}
		}
		return ctlResp{OK: true, Ready: true}
	case "stop":
		go d.Shutdown()
		return ctlResp{OK: true}
	default:
		return ctlResp{OK: false, Error: "unknown_op"}
	}
}

func (d *Daemon) applyActiveSnapshot() {
	if d.router == nil || d.store == nil {
		return
	}
	now := time.Now()
	snap, _, expired, err := d.store.ActiveSnapshot(now)
	if expired || (err != nil && ErrorCode(err) == "cache_expired") {
		d.router.SetCacheExpired(false)
		d.router.ReplaceSnapshot(mergeBootstrap(nil))
		return
	}
	if snap != nil {
		d.router.SetCacheExpired(false)
		d.router.ReplaceSnapshot(mergeBootstrap(snap))
		return
	}
	d.router.SetCacheExpired(false)
	d.router.ReplaceSnapshot(mergeBootstrap(nil))
}

func (d *Daemon) applyRefreshResult(snap *Snapshot, err error) {
	if err != nil {
		d.refreshErr = ErrorCode(err)
	} else {
		d.refreshErr = ""
	}
	if d.router == nil || d.store == nil {
		return
	}
	ps, lerr := d.store.Load()
	if lerr != nil {
		return
	}
	_, _, expired, _ := UsableSnapshot(ps, time.Now())
	if expired {
		d.router.SetCacheExpired(false)
		d.router.ReplaceSnapshot(mergeBootstrap(nil))
		return
	}
	if snap != nil {
		d.router.SetCacheExpired(false)
		d.router.ReplaceSnapshot(mergeBootstrap(snap))
	}
}

func (d *Daemon) publicStatus() PublicStatus {
	now := time.Now()
	var snap *Snapshot
	if d.store != nil {
		snap, _, _, _ = d.store.ActiveSnapshot(now)
	}
	if d.router != nil {
		st := d.router.Public(now, snap, d.refreshErr)
		if d.store != nil && d.store.ManagedActive() {
			st.Managed = true
			if doc, _ := d.store.LoadManaged(); doc != nil {
				st.Mode = doc.Mode
				st.PolicyVersion = doc.PolicyVersion
			}
		}
		return st
	}
	st := PublicStatus{
		ProxyEnabled: config.IsProxyEnabled(),
		Configured:   snap != nil,
		Route:        RouteDisabled,
	}
	if snap != nil {
		st.Nodes = len(snap.Nodes)
		st.Skipped = snap.Skipped
	}
	if d.store != nil && d.store.ManagedActive() {
		st.Managed = true
	}
	return st
}

func (d *Daemon) Shutdown() {
	d.shutdownOnce.Do(func() {
		d.mu.Lock()
		d.ready = false
		d.mu.Unlock()
		if d.cancel != nil {
			d.cancel()
		}
		d.stopProxyStack()
		if d.minerPID > 0 && d.StopMiner != nil {
			_ = d.StopMiner(d.minerPID)
		}
		_ = antisleep.Disable()
		if d.ctlLn != nil {
			_ = d.ctlLn.Close()
			d.ctlLn = nil
		}
	})
}

func acquireLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, privateFilePerm)
	if err != nil {
		return nil, errCode("lock_failed")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, errCode("already_running")
	}
	return f, nil
}

func writePID(path string, pid int) error {
	return atomicWrite(path, []byte(strconv.Itoa(pid)+"\n"), privateFilePerm)
}

func readPIDFile(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return pid, false
	}
	return pid, true
}

func IsSupervisorRunning() (int, bool) {
	dir, err := config.PrivateDir()
	if err != nil {
		return 0, false
	}
	return readPIDFile(filepath.Join(dir, "supervisor.pid"))
}

func DialControl(op string, timeout time.Duration) (*ctlResp, error) {
	dir, err := config.PrivateDir()
	if err != nil {
		return nil, err
	}
	sock := controlSocketPath(dir)
	conn, err := net.DialTimeout("unix", sock, timeout)
	if err != nil {
		return nil, errCode("not_running")
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if err := json.NewEncoder(conn).Encode(ctlReq{Op: op}); err != nil {
		return nil, errCode("control_io")
	}
	var resp ctlResp
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, errCode("control_io")
	}
	return &resp, nil
}

func StartAndWait(force bool, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if pid, ok := IsSupervisorRunning(); ok {
		if !force {
			resp, err := DialControl("ready", 3*time.Second)
			if err == nil && resp.Ready {
				fmt.Printf("tarish already running (supervisor pid %d)\n", pid)
				return nil
			}
		}
		_ = StopSupervisor()
		time.Sleep(200 * time.Millisecond)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	cmd := exec.Command(exe, "_miner-daemon")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	logDir := filepath.Join(func() string {
		d, _ := config.ConfigDir()
		return d
	}(), "log")
	_ = os.MkdirAll(logDir, 0o755)
	logPath := filepath.Join(logDir, "miner-daemon.log")
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		cmd.Stdout = lf
		cmd.Stderr = lf
	}
	if err := cmd.Start(); err != nil {
		return errCode("daemon_start")
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := DialControl("ready", 500*time.Millisecond)
		if err == nil && resp.Ready {
			fmt.Printf("tarish supervisor ready (pid %d)\n", cmd.Process.Pid)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errCode("ready_timeout")
}

func StopSupervisor() error {
	if resp, err := DialControl("stop", 3*time.Second); err == nil && resp.OK {
		time.Sleep(300 * time.Millisecond)
	}
	dir, err := config.PrivateDir()
	if err != nil {
		return err
	}
	pid, ok := readPIDFile(filepath.Join(dir, "supervisor.pid"))
	if ok {
		p, err := os.FindProcess(pid)
		if err == nil {
			_ = p.Signal(syscall.SIGTERM)
			time.Sleep(300 * time.Millisecond)
			_ = p.Signal(syscall.SIGKILL)
		}
	}
	_ = xmrig.StopOwnedFromPIDFile()
	_ = antisleep.Disable()
	return nil
}

func reconcileLegacyMiner() {
	pid, running := xmrig.IsRunning()
	if !running {
		return
	}
	if ownedXmrig(pid) {
		_ = xmrig.StopOwned(pid)
	}
}

func ownedXmrig(pid int) bool {
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	s := strings.ToLower(string(out))
	if !strings.Contains(s, "xmrig") {
		return false
	}
	if strings.Contains(s, "tarish") {
		return true
	}
	share := xmrig.GetBinPath()
	return share != "" && strings.Contains(s, strings.ToLower(share))
}
