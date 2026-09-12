package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
	"tarish/config"
)

func Handle(args []string) error {
	return HandleIO(args, os.Stdin, os.Stdout, os.Stderr)
}

func HandleIO(args []string, in io.Reader, out, errw io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(errw, "Usage: tarish proxy <configure|enable|disable|status|refresh|test> [--stdin]")
		return errCode("usage")
	}
	sub := strings.ToLower(args[0])
	rest := args[1:]
	switch sub {
	case "configure":
		return cmdConfigure(rest, in, out, errw)
	case "enable":
		return cmdEnable(out)
	case "disable":
		return cmdDisable(out)
	case "status":
		return cmdStatus(out)
	case "refresh":
		return cmdRefresh(out)
	case "test":
		return cmdTest(out)
	default:
		fmt.Fprintf(errw, "Unknown proxy command: %s\n", sub)
		return errCode("usage")
	}
}

func cmdConfigure(args []string, in io.Reader, out, errw io.Writer) error {
	stdin := false
	for _, a := range args {
		if a == "--stdin" {
			stdin = true
		}
	}
	store, err := OpenStore()
	if err != nil {
		return err
	}
	if store.ManagedActive() {
		PrintManagedRefuse(errw)
		return errCode("managed_active")
	}
	format, err := ParseFormat(config.GetProxyFormat())
	if err != nil {
		return err
	}

	if stdin {
		data, err := io.ReadAll(io.LimitReader(in, MaxDocumentBytes+1))
		if err != nil {
			return errCode("stdin_read")
		}
		if len(data) > MaxDocumentBytes {
			return errCode("document_too_large")
		}
		trimmed := strings.TrimSpace(string(data))
		if looksURL(trimmed) {
			return persistURL(store, trimmed, format, out)
		}
		snap, err := ParseDocument(data, format)
		if err != nil {
			return err
		}
		now := time.Now()
		snap = mergeBootstrap(snap)
		snap.FetchedAt = now
		snap.ValidatedAt = now
		ps := &persistedStore{
			Format:         string(snap.Format),
			StaticDocument: true,
			FetchedAt:      now,
			ValidatedAt:    now,
			Snapshot:       snapshotPersist(snap),
		}
		if err := store.Save(ps); err != nil {
			return err
		}
		_ = store.SaveCache(ps)
		fmt.Fprintf(out, "configured %d nodes (%d skipped); proxy remains disabled until enable\n", snap.NodeCount, snap.Skipped)
		return nil
	}

	fmt.Fprint(errw, "Subscription URL: ")
	url, err := readSecretLine(in)
	if err != nil {
		return err
	}
	return persistURL(store, url, format, out)
}

func looksURL(s string) bool {
	if strings.ContainsAny(s, "\n\r") {
		return false
	}
	return strings.HasPrefix(strings.ToLower(s), "https://")
}

func persistURL(store *Store, raw string, format Format, out io.Writer) error {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(raw), "https://") {
		return errCode("https_required")
	}
	ps := &persistedStore{
		SubscriptionURL: raw,
		Format:          string(format),
	}
	if err := store.Save(ps); err != nil {
		return err
	}
	f := NewFetcher(store, format)
	snap, err := f.Refresh(context.Background())
	if err != nil && snap == nil {
		return err
	}
	n := 0
	sk := 0
	if snap != nil {
		n = snap.NodeCount
		sk = snap.Skipped
	}
	fmt.Fprintf(out, "configured %d nodes (%d skipped); proxy remains disabled until enable\n", n, sk)
	if err != nil {
		fmt.Fprintf(out, "refresh warning: %s\n", ErrorCode(err))
	}
	return nil
}

func readSecretLine(in io.Reader) (string, error) {
	if f, ok := in.(*os.File); ok {
		if term.IsTerminal(int(f.Fd())) {
			b, err := term.ReadPassword(int(f.Fd()))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return "", errCode("input")
			}
			return strings.TrimSpace(string(b)), nil
		}
	}
	s, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && len(strings.TrimSpace(s)) == 0 {
		return "", errCode("input")
	}
	return strings.TrimSpace(s), nil
}

func cmdEnable(out io.Writer) error {
	store, err := OpenStore()
	if err != nil {
		return err
	}
	ps, err := store.Load()
	if err != nil {
		return err
	}
	if ps == nil || ps.Snapshot == nil || len(ps.Snapshot.Nodes) == 0 {
		now := time.Now()
		snap := mergeBootstrap(nil)
		snap.FetchedAt = now
		snap.ValidatedAt = now
		ps = &persistedStore{
			Format:         "auto",
			StaticDocument: true,
			FetchedAt:      now,
			ValidatedAt:    now,
			Snapshot:       snapshotPersist(snap),
		}
		if err := store.Save(ps); err != nil {
			return err
		}
	}
	if err := config.SetProxyEnabled(true); err != nil {
		return err
	}
	fmt.Fprintln(out, "proxy enabled")
	if _, ok := IsSupervisorRunning(); ok {
		resp, err := DialControl("enable", 15*time.Second)
		if err != nil {
			return err
		}
		if !resp.OK {
			return errCode(orCode(resp.Error, "enable_failed"))
		}
	}
	return nil
}

func cmdDisable(out io.Writer) error {
	if err := config.SetProxyEnabled(false); err != nil {
		return err
	}
	fmt.Fprintln(out, "proxy disabled")
	if _, ok := IsSupervisorRunning(); ok {
		resp, err := DialControl("disable", 15*time.Second)
		if err != nil {
			return err
		}
		if !resp.OK {
			return errCode(orCode(resp.Error, "disable_failed"))
		}
	}
	return nil
}

func orCode(code, fallback string) string {
	if strings.TrimSpace(code) == "" {
		return fallback
	}
	return code
}

func cmdStatus(out io.Writer) error {
	st, err := currentStatus()
	if err != nil {
		return err
	}
	printStatus(out, st)
	return nil
}

func cmdRefresh(out io.Writer) error {
	if _, ok := IsSupervisorRunning(); ok {
		resp, err := DialControl("refresh", 20*time.Second)
		if err != nil {
			return err
		}
		if resp.Status != nil {
			printStatus(out, *resp.Status)
		}
		if resp.Error != "" {
			fmt.Fprintf(out, "refresh: %s\n", resp.Error)
		}
		return nil
	}
	store, err := OpenStore()
	if err != nil {
		return err
	}
	f := NewFetcher(store, Format(config.GetProxyFormat()))
	snap, err := f.Refresh(context.Background())
	if snap != nil {
		fmt.Fprintf(out, "nodes=%d skipped=%d\n", snap.NodeCount, snap.Skipped)
	}
	if err != nil {
		fmt.Fprintf(out, "refresh: %s\n", ErrorCode(err))
	}
	return nil
}

func cmdTest(out io.Writer) error {
	if _, ok := IsSupervisorRunning(); ok {
		resp, err := DialControl("test", 20*time.Second)
		if err != nil {
			return err
		}
		if resp.Status != nil {
			printStatus(out, *resp.Status)
		}
		return nil
	}
	st, err := currentStatus()
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "supervisor not running; showing stored status only")
	printStatus(out, st)
	return nil
}

func currentStatus() (PublicStatus, error) {
	if _, ok := IsSupervisorRunning(); ok {
		resp, err := DialControl("status", 3*time.Second)
		if err == nil && resp.Status != nil {
			return *resp.Status, nil
		}
	}
	store, err := OpenStore()
	if err != nil {
		return PublicStatus{}, err
	}
	ps, err := store.Load()
	if err != nil {
		return PublicStatus{}, err
	}
	st := PublicStatus{
		ProxyEnabled: config.IsProxyEnabled(),
		Route:        RouteDisabled,
	}
	if ps != nil && ps.Snapshot != nil {
		st.Configured = len(ps.Snapshot.Nodes) > 0
		st.Nodes = len(ps.Snapshot.Nodes)
		st.Skipped = ps.Snapshot.Skipped
		stale, expired, age := CacheFreshness(ps.ValidatedAt, time.Now())
		st.Stale = stale
		st.Expired = expired
		st.CacheAge = FormatAge(age)
		st.ErrorCode = ps.LastErrorCode
		st.NodeStatuses = snapshotPublicStatuses(ps.Snapshot.toSnapshot())
	}
	if store.ManagedActive() {
		st.Managed = true
		if doc, _ := store.LoadManaged(); doc != nil {
			st.Mode = doc.Mode
			st.PolicyVersion = doc.PolicyVersion
		}
	}
	if !st.ProxyEnabled {
		st.Route = RouteDisabled
	} else if !st.Configured {
		st.Route = RouteWaiting
	}
	return st, nil
}

func printStatus(out io.Writer, st PublicStatus) {
	fmt.Fprintf(out, "proxy: %s\n", boolWord(st.ProxyEnabled, "enabled", "disabled"))
	fmt.Fprintf(out, "configured: %s\n", boolWord(st.Configured, "yes", "no"))
	fmt.Fprintf(out, "route: %s\n", st.Route)
	fmt.Fprintf(out, "nodes: %d\n", st.Nodes)
	fmt.Fprintf(out, "skipped: %d\n", st.Skipped)
	if st.CacheAge != "" {
		fmt.Fprintf(out, "cache_age: %s\n", st.CacheAge)
	}
	fmt.Fprintf(out, "stale: %s\n", boolWord(st.Stale, "yes", "no"))
	fmt.Fprintf(out, "expired: %s\n", boolWord(st.Expired, "yes", "no"))
	if st.Managed {
		fmt.Fprintf(out, "managed: yes\n")
		if st.Mode != "" {
			fmt.Fprintf(out, "mode: %s\n", st.Mode)
		}
	}
	if st.ErrorCode != "" {
		fmt.Fprintf(out, "error: %s\n", st.ErrorCode)
	}
	for _, n := range st.NodeStatuses {
		fmt.Fprintf(out, "node %s health=%s", n.ID, n.Health)
		if n.LastErrorCode != "" {
			fmt.Fprintf(out, " error=%s", n.LastErrorCode)
		}
		fmt.Fprintln(out)
	}
}

func boolWord(v bool, yes, no string) string {
	if v {
		return yes
	}
	return no
}
