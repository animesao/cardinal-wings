// sftp.go implements a full SSH/SFTP server inside wings. The panel assigns a
// username/password per container (bcrypt-hashed on disk); every SFTP session
// is jailed to that container's data directory on the host filesystem, so
// users can point Filezilla / WinSCP straight at their container without an
// sshd running inside the image.
//
// Data layout (same host as wings — wings spawns `cardinal serve` locally):
//
//	$CARDINAL_DATA_DIR/overlay/<container-id>/merged   <- container rootfs
//	merged/data               <- itzg-style images (Minecraft etc.)
//	merged/home/container     <- Pterodactyl-style images
//
// If the container is stopped the merged overlay stays mounted, so files
// remain reachable. After a host reboot the overlay is only remounted when
// the container starts; in that case wings temporarily starts the container
// for the duration of the session and stops it again afterwards.
package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"encoding/pem"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"

	"github.com/animesao/cardinal-wings/internal/config"
)

// ─── Credential store ────────────────────────────────────

// sftpEntry is one container's SFTP credential, stored bcrypt-hashed.
type sftpEntry struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
	ContainerID  string `json:"container_id"`
}

// sftpStore persists SFTP users to a JSON file next to the audit log.
type sftpStore struct {
	mu   sync.RWMutex
	path string
	byID map[string]*sftpEntry // container id → entry
}

func loadSFTPStore() (*sftpStore, error) {
	dir := wingsDataDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &sftpStore{
		path: filepath.Join(dir, "sftp-users.json"),
		byID: map[string]*sftpEntry{},
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(data, &s.byID)
	if s.byID == nil {
		s.byID = map[string]*sftpEntry{}
	}
	return s, nil
}

func (s *sftpStore) save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.byID, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// set upserts the credential for a container.
func (s *sftpStore) set(containerID, username, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.byID[containerID] = &sftpEntry{Username: username, PasswordHash: string(hash), ContainerID: containerID}
	s.mu.Unlock()
	return s.save()
}

func (s *sftpStore) remove(containerID string) error {
	s.mu.Lock()
	delete(s.byID, containerID)
	s.mu.Unlock()
	return s.save()
}

func (s *sftpStore) entryByUsername(username string) (*sftpEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.byID {
		if e.Username == username {
			return e, true
		}
	}
	return nil, false
}

func (s *sftpStore) entryByContainer(containerID string) (*sftpEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.byID[containerID]
	return e, ok
}

func wingsDataDir() string {
	if dir := os.Getenv("WINGS_DATA_DIR"); dir != "" {
		return dir
	}
	return "."
}

// ─── Host key ────────────────────────────────────────────

// loadHostKey loads (or generates) the SSH host key used by the SFTP server.
// It is persisted so clients do not see a new fingerprint on every restart.
func loadHostKey() (ssh.Signer, error) {
	dir := wingsDataDir()
	keyPath := filepath.Join(dir, "sftp_host_key")
	if pem, err := os.ReadFile(keyPath); err == nil {
		if signer, err := ssh.ParsePrivateKey(pem); err == nil {
			return signer, nil
		}
		// Corrupt key — fall through and regenerate.
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	pem, err := marshalEd25519PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, pem, 0600); err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(priv)
}

// marshalEd25519PrivateKey serializes an ed25519 private key for disk.
func marshalEd25519PrivateKey(priv ed25519.PrivateKey) ([]byte, error) {
	block, err := ssh.MarshalPrivateKey(priv, "cardinal-wings sftp")
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(block), nil
}

// sftpTimeoutCtx is a short-lived context for best-effort container
// start/stop around an SFTP session.
func sftpTimeoutCtx(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// ─── Data root resolution ────────────────────────────────

func cardinalDataDir() string {
	if dir := os.Getenv("CARDINAL_DATA_DIR"); dir != "" {
		return dir
	}
	if os.Getuid() == 0 {
		return "/root/.cardinal"
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cardinal")
}

// containerDataRoot returns the host path of a container's data directory:
// merged/data for itzg-style images, merged/home/container for Pterodactyl
// images, falling back to the merged root itself. It also reports whether the
// overlay is currently mounted (i.e. the directory exists and has content).
func containerDataRoot(containerID string) (string, bool, error) {
	merged := filepath.Join(cardinalDataDir(), "overlay", containerID, "merged")
	if _, err := os.Stat(merged); err != nil {
		return "", false, fmt.Errorf("container data not available: %w", err)
	}
	for _, sub := range []string{"data", "home/container"} {
		p := filepath.Join(merged, sub)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p, true, nil
		}
	}
	entries, err := os.ReadDir(merged)
	if err != nil {
		return "", false, err
	}
	return merged, len(entries) > 0, nil
}

// ─── Jailed filesystem ───────────────────────────────────

// jailFS implements pkg/sftp's request handlers against a fixed root
// directory. Every client path is cleaned and clamped inside the root, so a
// session can never reach outside the container's data directory.
type jailFS struct {
	root string
}

// resolve clamps a client-supplied path inside the jail root.
func (fs jailFS) resolve(p string) (string, error) {
	if p == "" {
		p = "/"
	}
	clean := path.Clean("/" + p)
	full := filepath.Join(fs.root, filepath.FromSlash(clean))
	rel, err := filepath.Rel(fs.root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", os.ErrPermission
	}
	return full, nil
}

func (fs jailFS) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	p, err := fs.resolve(r.Filepath)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (fs jailFS) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	p, err := fs.resolve(r.Filepath)
	if err != nil {
		return nil, err
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	pf := r.Pflags()
	if pf.Append {
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	return os.OpenFile(p, flags, 0644)
}

func (fs jailFS) Filecmd(r *sftp.Request) error {
	p, err := fs.resolve(r.Filepath)
	if err != nil {
		return err
	}
	switch r.Method {
	case "Mkdir":
		return os.Mkdir(p, 0755)
	case "Rmdir":
		return os.Remove(p)
	case "Remove":
		return os.Remove(p)
	case "Rename":
		dst, err := fs.resolve(r.Target)
		if err != nil {
			return err
		}
		return os.Rename(p, dst)
	case "Symlink":
		dst, err := fs.resolve(r.Target)
		if err != nil {
			return err
		}
		return os.Symlink(p, dst)
	case "Truncate":
		attrs := r.Attributes()
		return os.Truncate(p, int64(attrs.Size))
	case "Setstat":
		return fs.setstat(p, r)
	default:
		return fmt.Errorf("unsupported filecmd %q", r.Method)
	}
}

// setstat applies the subset of SSH_FXP_SETSTAT wings supports: permissions
// and mtime. Ownership changes are skipped (files stay owned by the
// container's runtime user).
func (fs jailFS) setstat(p string, r *sftp.Request) error {
	flags := r.AttrFlags()
	attrs := r.Attributes()
	if flags.Permissions {
		if err := os.Chmod(p, attrs.FileMode()); err != nil {
			return err
		}
	}
	if flags.Acmodtime {
		if err := os.Chtimes(p, attrs.AccessTime(), attrs.ModTime()); err != nil {
			return err
		}
	}
	return nil
}

func (fs jailFS) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	p, err := fs.resolve(r.Filepath)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	infos := make([]os.FileInfo, 0, len(entries))
	for _, de := range entries {
		if fi, err := de.Info(); err == nil {
			infos = append(infos, fi)
		}
	}
	return listerAt(infos), nil
}

func (fs jailFS) Stat(r *sftp.Request) (os.FileInfo, error) {
	p, err := fs.resolve(r.Filepath)
	if err != nil {
		return nil, err
	}
	return os.Stat(p)
}

func (fs jailFS) Lstat(r *sftp.Request) (os.FileInfo, error) {
	p, err := fs.resolve(r.Filepath)
	if err != nil {
		return nil, err
	}
	return os.Lstat(p)
}

func (fs jailFS) Readlink(r *sftp.Request) (string, error) {
	p, err := fs.resolve(r.Filepath)
	if err != nil {
		return "", err
	}
	return os.Readlink(p)
}

func (fs jailFS) Realpath(r *sftp.Request) (string, error) {
	clean := path.Clean("/" + r.Filepath)
	return clean, nil
}

// listerAt adapts []os.FileInfo to sftp.ListerAt.
type listerAt []os.FileInfo

func (l listerAt) ListAt(ls []os.FileInfo, off int64) (int, error) {
	if off >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(ls, l[off:])
	if n < len(ls) {
		return n, io.EOF
	}
	return n, nil
}

// ─── SFTP server loop ────────────────────────────────────

var (
	sftpStoreInst *sftpStore
	sftpCfgPort   int
)

// startSFTPServer launches the SSH/SFTP listener in a background goroutine.
// It never fails the daemon: if the port is busy wings keeps running and logs
// the problem (the panel reports SFTP as unavailable on this node).
func startSFTPServer(cfg *config.Config) error {
	if !cfg.Server.SFTPEnabled {
		return nil
	}
	store, err := loadSFTPStore()
	if err != nil {
		return fmt.Errorf("load sftp store: %w", err)
	}
	sftpStoreInst = store
	sftpCfgPort = cfg.Server.SFTPPort

	signer, err := loadHostKey()
	if err != nil {
		return fmt.Errorf("sftp host key: %w", err)
	}

	sshCfg := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			entry, ok := store.entryByUsername(conn.User())
			if !ok {
				return nil, errors.New("unknown sftp user")
			}
			if bcrypt.CompareHashAndPassword([]byte(entry.PasswordHash), pass) != nil {
				return nil, errors.New("invalid password")
			}
			return &ssh.Permissions{Extensions: map[string]string{"container-id": entry.ContainerID}}, nil
		},
		MaxAuthTries: 3,
	}
	sshCfg.AddHostKey(signer)

	addr := net.JoinHostPort(cfg.Server.SFTPHost, fmt.Sprintf("%d", cfg.Server.SFTPPort))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("sftp listen %s: %w", addr, err)
	}

	go func() {
		logf("sftp server listening on %s", addr)
		for {
			conn, err := ln.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}
			go handleSSHConn(conn, sshCfg, store)
		}
	}()
	return nil
}

func handleSSHConn(conn net.Conn, sshCfg *ssh.ServerConfig, store *sftpStore) {
	defer conn.Close()
	sconn, chans, reqs, err := ssh.NewServerConn(conn, sshCfg)
	if err != nil {
		return
	}
	defer sconn.Close()
	go ssh.DiscardRequests(reqs)

	var containerID string
	if perm := sconn.Permissions; perm != nil {
		containerID = perm.Extensions["container-id"]
	}
	if containerID == "" {
		return
	}

	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			_ = newChan.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}
		ch, chReqs, err := newChan.Accept()
		if err != nil {
			continue
		}
		go handleSSHSession(ch, chReqs, containerID)
	}
}

// handleSSHSession services one SSH session channel: it waits for the sftp
// subsystem request, prepares the jailed filesystem (temporarily starting a
// stopped container whose overlay is not mounted), then serves SFTP until the
// channel closes.
func handleSSHSession(ch ssh.Channel, reqs <-chan *ssh.Request, containerID string) {
	defer ch.Close()

	for req := range reqs {
		if req.Type != "subsystem" || !strings.Contains(string(req.Payload), "sftp") {
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
			continue
		}
		if req.WantReply {
			_ = req.Reply(true, nil)
		}

		root, mounted, err := containerDataRoot(containerID)
		startedTemporarily := false
		if err != nil || !mounted {
			// Overlay not mounted (e.g. after host reboot): start the
			// container for the duration of the session, then stop it again.
			startedTemporarily = tryStartContainer(containerID)
			root, mounted, err = containerDataRoot(containerID)
			// The overlay mounts asynchronously after the init starts; poll
			// briefly for the data dir to appear before giving up.
			for i := 0; (err != nil || !mounted) && i < 20; i++ {
				time.Sleep(500 * time.Millisecond)
				root, mounted, err = containerDataRoot(containerID)
			}
			if err != nil || !mounted {
				_, _ = io.WriteString(ch, "sftp: container data unavailable\r\n")
				if startedTemporarily {
					tryStopContainer(containerID)
				}
				return
			}
		}

		fs := jailFS{root: root}
		server := sftp.NewRequestServer(ch, sftp.Handlers{
			FileGet:  fs,
			FilePut:  fs,
			FileCmd:  fs,
			FileList: fs,
		})
		if err := server.Serve(); err != nil && !errors.Is(err, io.EOF) {
			logf("sftp session for %s ended: %v", containerID, err)
		}

		if startedTemporarily {
			tryStopContainer(containerID)
		}
		return
	}
}

// tryStartContainer asks the local cardinal node to start a container. It
// returns whether the start was issued (best-effort; failures are logged).
func tryStartContainer(containerID string) bool {
	if defaultClient == nil {
		return false
	}
	ctx, cancel := sftpTimeoutCtx(20 * time.Second)
	defer cancel()
	if err := defaultClient.Action(ctx, containerID, "start"); err != nil {
		logf("sftp: temporary start of %s failed: %v", containerID, err)
		return false
	}
	return true
}

func tryStopContainer(containerID string) {
	if defaultClient == nil {
		return
	}
	ctx, cancel := sftpTimeoutCtx(20 * time.Second)
	defer cancel()
	if err := defaultClient.Action(ctx, containerID, "stop"); err != nil {
		logf("sftp: temporary stop of %s failed: %v", containerID, err)
	}
}

// ─── Admin API ───────────────────────────────────────────

// handleSftpInfo reports the listener status. Public read; the panel uses it
// to show the connect address in server settings.
func handleSftpInfo(w http.ResponseWriter, r *http.Request) {
	enabled := sftpStoreInst != nil
	port := sftpCfgPort
	if !enabled {
		port = 0
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled": enabled,
		"host":    listenerHost(),
		"port":    port,
	})
}

// listenerHost reports the address the SFTP server is bound to. This is
// informational only; the panel prefers the node's configured public host.
func listenerHost() string {
	if sftpCfgPort == 0 {
		return ""
	}
	return "0.0.0.0"
}

// handleSftpStatus returns whether a container has SFTP credentials.
func handleSftpStatus(w http.ResponseWriter, r *http.Request, containerID string) {
	if sftpStoreInst == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"enabled": false})
		return
	}
	e, ok := sftpStoreInst.entryByContainer(containerID)
	username := ""
	if ok {
		username = e.Username
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":  ok,
		"username": username,
	})
}

// handleSftpSet creates/updates the SFTP credential for a container.
// Body: { username, password }
func handleSftpSet(w http.ResponseWriter, r *http.Request, containerID string) {
	if sftpStoreInst == nil {
		writeErr(w, http.StatusConflict, ErrConflict, "sftp server is disabled on this node")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" || len(body.Username) > 64 {
		writeError(w, http.StatusBadRequest, "username required (max 64 chars)")
		return
	}
	if len(body.Password) < 6 || len(body.Password) > 128 {
		writeError(w, http.StatusBadRequest, "password must be 6-128 chars")
		return
	}

	// The container must exist on this node before we mint credentials for it.
	if defaultClient != nil {
		ctx, cancel := sftpTimeoutCtx(10 * time.Second)
		defer cancel()
		if _, err := defaultClient.Inspect(ctx, containerID); err != nil {
			writeErr(w, http.StatusNotFound, ErrNotFound, "container %s not found: %s", containerID, err.Error())
			return
		}
	}

	if err := sftpStoreInst.set(containerID, body.Username, body.Password); err != nil {
		writeErr(w, http.StatusInternalServerError, ErrInternal, "save sftp credential: %s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       true,
		"username": body.Username,
		"port":     sftpCfgPort,
	})
}

// handleSftpDelete removes the SFTP credential for a container.
func handleSftpDelete(w http.ResponseWriter, r *http.Request, containerID string) {
	if sftpStoreInst != nil {
		if err := sftpStoreInst.remove(containerID); err != nil {
			writeErr(w, http.StatusInternalServerError, ErrInternal, "remove sftp credential: %s", err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}
