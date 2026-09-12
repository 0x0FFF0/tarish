package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"tarish/snellspec"
)

const managedFile = "managed.json"

type ApplyHooks struct {
	ReplaceSnapshot  func(*Snapshot)
	Now              func() time.Time
	NotifySupervisor func() error
}

func (s *Store) managedPath() string {
	return filepath.Join(s.dir, managedFile)
}

func (s *Store) HasManaged() bool {
	fi, err := os.Lstat(s.managedPath())
	return err == nil && !fi.IsDir()
}

func (s *Store) LoadManaged() (*snellspec.ManagedDocument, error) {
	path := s.managedPath()
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil, nil
	}
	data, err := readPrivateFile(path)
	if err != nil {
		return nil, err
	}
	var doc snellspec.ManagedDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, errCode("malformed_store")
	}
	RememberSecrets(doc.Snapshot(), "")
	return &doc, nil
}

func (s *Store) SaveManaged(doc *snellspec.ManagedDocument) error {
	if doc == nil {
		return errCode("invalid_snapshot")
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return errCode("store_io")
	}
	if err := atomicWrite(s.managedPath(), data, privateFilePerm); err != nil {
		return err
	}
	RememberSecrets(doc.Snapshot(), "")
	return nil
}

func (s *Store) ClearManaged() error {
	path := s.managedPath()
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return errCode("store_io")
	}
	return nil
}

func (s *Store) ManagedActive() bool {
	doc, err := s.LoadManaged()
	if err != nil || doc == nil {
		return false
	}
	return doc.Mode == snellspec.ModeInherit || doc.Mode == snellspec.ModeCustom
}

func ApplyPending(body []byte, hooks ApplyHooks) (bool, error) {
	var doc snellspec.ManagedDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return false, errCode("invalid_snapshot")
	}
	store, err := OpenStore()
	if err != nil {
		return false, err
	}
	return ApplyManagedSnapshot(store, &doc, hooks)
}

func ApplyManagedSnapshot(store *Store, doc *snellspec.ManagedDocument, hooks ApplyHooks) (bool, error) {
	if store == nil || doc == nil {
		return false, errCode("invalid_snapshot")
	}
	now := time.Now()
	if hooks.Now != nil {
		now = hooks.Now()
	}
	if doc.ProtocolVersion != 0 && doc.ProtocolVersion != snellspec.ProtocolVersion {
		return false, errCode("unsupported_protocol")
	}
	if doc.ProtocolVersion == 0 {
		doc.ProtocolVersion = snellspec.ProtocolVersion
	}

	current, err := store.LoadManaged()
	if err != nil {
		return false, err
	}
	if current != nil && doc.PolicyVersion < current.PolicyVersion {
		return false, errCode("stale_version")
	}
	if current != nil && doc.PolicyVersion == current.PolicyVersion {
		return false, nil
	}

	if doc.Mode == snellspec.ModeLocal || !doc.Supported && doc.Mode == "" {
		if err := store.ClearManaged(); err != nil {
			return false, err
		}
		notifyManagedApply(store, hooks, loadLocalOrSeed(store, now))
		return true, nil
	}

	if err := validateManagedDocument(doc); err != nil {
		return false, err
	}

	if err := store.SaveManaged(doc); err != nil {
		return false, err
	}

	snap := usableManagedSnapshot(doc, now)
	notifyManagedApply(store, hooks, snap)
	return true, nil
}

func validateManagedDocument(doc *snellspec.ManagedDocument) error {
	if doc.Mode != snellspec.ModeInherit && doc.Mode != snellspec.ModeCustom && doc.Mode != snellspec.ModeLocal {
		return errCode("invalid_snapshot")
	}
	seen := map[string]bool{}
	for i, w := range doc.Nodes {
		n := w.Spec()
		if n.Host == "" || n.PSK == "" || n.Port < 1 {
			return errCode("invalid_snapshot")
		}
		if _, _, err := CanonicalHostPort(n.Host, n.Port); err != nil {
			return errCode("invalid_snapshot")
		}
		n = snellspec.FinalizeNode(n)
		doc.Nodes[i] = snellspec.ToWireNode(n)
		if seen[n.ID] {
			continue
		}
		seen[n.ID] = true
	}
	return nil
}

func usableManagedSnapshot(doc *snellspec.ManagedDocument, now time.Time) *Snapshot {
	if doc == nil {
		return mergeBootstrap(nil)
	}
	_, expired, _ := CacheFreshness(doc.ValidatedAt, now)
	if expired || !doc.Enabled {
		return mergeBootstrap(nil)
	}
	return mergeBootstrap(doc.Snapshot())
}

func loadLocalOrSeed(store *Store, now time.Time) *Snapshot {
	ps, err := store.Load()
	if err != nil || ps == nil {
		return mergeBootstrap(nil)
	}
	snap, _, expired, _ := UsableSnapshot(ps, now)
	if expired || snap == nil {
		return mergeBootstrap(nil)
	}
	return mergeBootstrap(snap)
}

func notifyManagedApply(store *Store, hooks ApplyHooks, snap *Snapshot) {
	if hooks.ReplaceSnapshot != nil {
		hooks.ReplaceSnapshot(snap)
		return
	}
	if hooks.NotifySupervisor != nil {
		_ = hooks.NotifySupervisor()
		return
	}
	if _, ok := IsSupervisorRunning(); ok {
		_, _ = DialControl("reload-managed", 5*time.Second)
	}
}

func CurrentPublicStatus() (PublicStatus, error) {
	st, err := currentStatus()
	if err != nil {
		return st, err
	}
	if store, err := OpenStore(); err == nil && store.ManagedActive() {
		st.Managed = true
		if doc, _ := store.LoadManaged(); doc != nil {
			st.Mode = doc.Mode
		}
	}
	return st, nil
}

func PrintManagedRefuse(errw io.Writer) {
	fmt.Fprintln(errw, "proxy is panel-managed; exit managed mode on the dashboard before configure")
}

func managedConfigureBlocked(store *Store) bool {
	if store == nil {
		return false
	}
	return store.ManagedActive()
}

func (s *Store) ActiveSnapshot(now time.Time) (*Snapshot, bool, bool, error) {
	if doc, err := s.LoadManaged(); err == nil && doc != nil && (doc.Mode == snellspec.ModeInherit || doc.Mode == snellspec.ModeCustom) {
		_, expired, _ := CacheFreshness(doc.ValidatedAt, now)
		if expired || !doc.Enabled {
			return mergeBootstrap(nil), true, true, errCode("cache_expired")
		}
		snap := mergeBootstrap(doc.Snapshot())
		stale, _, _ := CacheFreshness(doc.ValidatedAt, now)
		return snap, stale, false, nil
	}
	ps, err := s.Load()
	if err != nil {
		return nil, false, true, err
	}
	return UsableSnapshot(ps, now)
}
