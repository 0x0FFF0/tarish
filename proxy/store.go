package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"tarish/config"
	"tarish/userctx"
)

const (
	storeVersion     = 1
	privateDirPerm   = 0o700
	privateFilePerm  = 0o600
	subscriptionFile = "subscription.json"
	cacheFile        = "cache.json"
	runtimeSubdir    = "runtime"
)

type persistedStore struct {
	Version         int                `json:"version"`
	SubscriptionURL string             `json:"subscription_url,omitempty"`
	Format          string             `json:"format"`
	StaticDocument  bool               `json:"static_document,omitempty"`
	ETag            string             `json:"etag,omitempty"`
	LastModified    string             `json:"last_modified,omitempty"`
	FetchedAt       time.Time          `json:"fetched_at,omitempty"`
	ValidatedAt     time.Time          `json:"validated_at,omitempty"`
	Snapshot        *persistedSnapshot `json:"snapshot,omitempty"`
	LastErrorCode   string             `json:"last_error_code,omitempty"`
}

type persistedSnapshot struct {
	Nodes       []NodeSpec   `json:"nodes"`
	Skipped     int          `json:"skipped"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	Format      Format       `json:"format"`
}

type Store struct {
	dir string
}

func PrivatePath() (string, error) {
	return config.PrivateDir()
}

func OpenStore() (*Store, error) {
	dir, err := config.PrivateDir()
	if err != nil {
		return nil, err
	}
	if err := ensurePrivateTree(dir); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func ensurePrivateTree(dir string) error {
	if err := mkdirPrivate(dir); err != nil {
		return err
	}
	if err := mkdirPrivate(filepath.Join(dir, runtimeSubdir)); err != nil {
		return err
	}
	return nil
}

func mkdirPrivate(dir string) error {
	if err := os.MkdirAll(dir, privateDirPerm); err != nil {
		return errCode("store_io")
	}
	if err := os.Chmod(dir, privateDirPerm); err != nil {
		return errCode("store_io")
	}
	return checkPath(dir, true)
}

func (s *Store) Dir() string { return s.dir }

func (s *Store) RuntimeDir() string {
	return filepath.Join(s.dir, runtimeSubdir)
}

func (s *Store) Load() (*persistedStore, error) {
	path := filepath.Join(s.dir, subscriptionFile)
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil, nil
	}
	data, err := readPrivateFile(path)
	if err != nil {
		return nil, err
	}
	var ps persistedStore
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, errCode("malformed_store")
	}
	if ps.Version != storeVersion {
		return nil, errCode("malformed_store")
	}
	RememberSecrets(ps.Snapshot.toSnapshot(), ps.SubscriptionURL)
	return &ps, nil
}

func (s *Store) Save(ps *persistedStore) error {
	ps.Version = storeVersion
	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return errCode("store_io")
	}
	if err := atomicWrite(filepath.Join(s.dir, subscriptionFile), data, privateFilePerm); err != nil {
		return err
	}
	RememberSecrets(ps.Snapshot.toSnapshot(), ps.SubscriptionURL)
	return nil
}

func (s *Store) SaveCache(ps *persistedStore) error {
	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return errCode("store_io")
	}
	return atomicWrite(filepath.Join(s.dir, cacheFile), data, privateFilePerm)
}

func (s *Store) LoadCache() (*persistedStore, error) {
	path := filepath.Join(s.dir, cacheFile)
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil, nil
	}
	data, err := readPrivateFile(path)
	if err != nil {
		return nil, err
	}
	var ps persistedStore
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, errCode("malformed_store")
	}
	return &ps, nil
}

func (s *Store) HasConfig() bool {
	_, err := os.Lstat(filepath.Join(s.dir, subscriptionFile))
	return err == nil
}

func (p *persistedSnapshot) toSnapshot() *Snapshot {
	if p == nil {
		return nil
	}
	return &Snapshot{
		Nodes:       append([]NodeSpec(nil), p.Nodes...),
		Skipped:     p.Skipped,
		Diagnostics: append([]Diagnostic(nil), p.Diagnostics...),
		Format:      p.Format,
		NodeCount:   len(p.Nodes),
	}
}

func snapshotPersist(s *Snapshot) *persistedSnapshot {
	if s == nil {
		return nil
	}
	return &persistedSnapshot{
		Nodes:       append([]NodeSpec(nil), s.Nodes...),
		Skipped:     s.Skipped,
		Diagnostics: append([]Diagnostic(nil), s.Diagnostics...),
		Format:      s.Format,
	}
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := checkParent(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return errCode("store_io")
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return errCode("store_io")
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return errCode("store_io")
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return errCode("store_io")
	}
	if err := tmp.Close(); err != nil {
		return errCode("store_io")
	}
	if err := os.Rename(tmpName, path); err != nil {
		return errCode("atomic_write_failed")
	}
	return checkPath(path, false)
}

func readPrivateFile(path string) ([]byte, error) {
	if err := checkPath(path, false); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func checkParent(path string) error {
	return checkPath(filepath.Dir(path), true)
}

func checkPath(path string, dir bool) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return errCode("store_io")
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return errCode("symlink_rejected")
	}
	if dir && !fi.IsDir() {
		return errCode("store_io")
	}
	if !dir && fi.IsDir() {
		return errCode("store_io")
	}
	if dir {
		if fi.Mode().Perm()&0o077 != 0 {
			_ = os.Chmod(path, privateDirPerm)
			fi, err = os.Lstat(path)
			if err != nil {
				return errCode("store_io")
			}
			if fi.Mode().Perm()&0o077 != 0 {
				return errCode("insecure_permissions")
			}
		}
	} else if fi.Mode().Perm()&0o177 != 0 {
		_ = os.Chmod(path, privateFilePerm)
		fi, err = os.Lstat(path)
		if err != nil {
			return errCode("store_io")
		}
		if fi.Mode().Perm()&0o177 != 0 {
			return errCode("insecure_permissions")
		}
	}
	want, err := allowedUID()
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if int(st.Uid) != want && os.Geteuid() != 0 {
		return errCode("unexpected_owner")
	}
	if os.Geteuid() == 0 && int(st.Uid) != want && int(st.Uid) != 0 {
		return errCode("unexpected_owner")
	}
	return nil
}

func allowedUID() (int, error) {
	if os.Geteuid() != 0 {
		return os.Getuid(), nil
	}
	identity, err := userctx.Current()
	if err != nil || identity.Username == "" {
		return os.Getuid(), nil
	}
	u, err := user.Lookup(identity.Username)
	if err != nil {
		return os.Getuid(), nil
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return os.Getuid(), nil
	}
	return uid, nil
}

func (s *Store) WriteRuntime(name string, data []byte) (string, error) {
	dir := s.RuntimeDir()
	if err := mkdirPrivate(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if err := atomicWrite(path, data, privateFilePerm); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Store) ControlSocketPath() string {
	return controlSocketPath(s.dir)
}

func controlSocketPath(privateDir string) string {
	sum := sha256.Sum256([]byte(privateDir))
	return filepath.Join("/tmp", "tsh-"+hex.EncodeToString(sum[:8])+".sock")
}

func (s *Store) LockPath() string {
	return filepath.Join(s.dir, "supervisor.lock")
}

func (s *Store) SupervisorPIDPath() string {
	return filepath.Join(s.dir, "supervisor.pid")
}

func (s *Store) MinerPIDPath() string {
	return filepath.Join(s.dir, "miner.pid")
}

func (s *Store) TokenPath() string {
	return filepath.Join(s.dir, "http_token")
}

func CacheFreshness(validated time.Time, now time.Time) (stale, expired bool, age time.Duration) {
	if validated.IsZero() {
		return true, true, 0
	}
	age = now.Sub(validated)
	if age < 0 {
		age = 0
	}
	expired = age > MaxCacheAge
	stale = age > StaleAfter
	return stale, expired, age
}

func FormatAge(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}
