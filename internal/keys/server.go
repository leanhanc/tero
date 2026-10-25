package keys

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/leanhanc/tero/internal/config"
	"github.com/leanhanc/tero/internal/hostfs"
	"github.com/leanhanc/tero/internal/journal"
	"github.com/leanhanc/tero/internal/store"
)

const (
	// User is the system user the key service runs as.
	User = "tero-keys"
	// SocketPath is where tero-keys.socket listens.
	SocketPath = "/run/tero-keys/keys.sock"

	// maxConnections, idleTimeout and requestTimeout bound what one caller
	// can tie up. Each caller has its own connections, so a flood from the
	// dashboard can't keep root's commands out.
	maxConnections = 32
	idleTimeout    = 5 * time.Second
	requestTimeout = 30 * time.Second

	pruneInterval = time.Hour
	flushInterval = time.Minute
)

// keyFilePattern matches the credential names systemd gives the files of
// /etc/tero/keys/: the files are named by version, and
// LoadCredentialEncrypted=kek:/etc/tero/keys/ names each one's credential
// kek_<file name>, which is also the name sealed into the file.
var keyFilePattern = regexp.MustCompile(`^kek_([1-9][0-9]{0,8})$`)

// Run is `tero keys`, started by systemd through tero-keys.socket.
func Run(ctx context.Context) error {
	self, err := user.Lookup(User)
	if err != nil {
		return fmt.Errorf("find the %s user: %w", User, err)
	}
	if strconv.Itoa(os.Getuid()) != self.Uid {
		return fmt.Errorf("tero keys only runs as %s, started by systemd", User)
	}

	tero, err := user.Lookup("tero")
	if err != nil {
		return fmt.Errorf("find the tero user: %w", err)
	}
	teroUID, _ := strconv.Atoi(tero.Uid)

	keys, err := LoadKeys(os.Getenv("CREDENTIALS_DIRECTORY"))
	if err != nil {
		return err
	}

	listener, err := activatedListener()
	if err != nil {
		return err
	}
	defer listener.Close()

	db, err := store.OpenWith(ctx, DatabasePath, Migrations)
	if err != nil {
		return err
	}
	defer db.Close()

	cfg, err := config.Load(hostfs.Host)
	if err != nil {
		return err
	}
	service, err := NewService(db, keys, Options{Domain: cfg.DashboardDomain, Journal: journal.Send})
	if err != nil {
		return err
	}
	go service.maintain(ctx)

	return service.Serve(ctx, listener, peers{teroUID: teroUID, dashboard: dashboardCgroup})
}

// LoadKeys reads every master key version systemd decrypted into dir.
func LoadKeys(dir string) (map[int][]byte, error) {
	if dir == "" {
		return nil, errors.New("no CREDENTIALS_DIRECTORY: the master key is only handed over by systemd")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	keys := map[int][]byte{}
	for _, entry := range entries {
		match := keyFilePattern.FindStringSubmatch(entry.Name())
		if match == nil {
			continue
		}

		key, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if len(key) != keySize {
			return nil, fmt.Errorf("master key %s is %d bytes, not %d", entry.Name(), len(key), keySize)
		}

		version, _ := strconv.Atoi(match[1])
		keys[version] = key
	}

	if len(keys) == 0 {
		return nil, errors.New("systemd handed over no master key")
	}

	return keys, nil
}

// activatedListener is the socket systemd passed as file descriptor 3.
func activatedListener() (*net.UnixListener, error) {
	isForUs := os.Getenv("LISTEN_PID") == strconv.Itoa(os.Getpid()) && os.Getenv("LISTEN_FDS") == "1"
	if !isForUs {
		return nil, errors.New("tero keys must be started by tero-keys.socket")
	}

	file := os.NewFile(3, "tero-keys.socket")
	listener, err := net.FileListener(file)
	file.Close()
	if err != nil {
		return nil, err
	}

	unixListener, isUnix := listener.(*net.UnixListener)
	if !isUnix {
		listener.Close()
		return nil, errors.New("tero-keys.socket is not a Unix socket")
	}

	return unixListener, nil
}

// Serve answers connections until ctx is cancelled, and returns once the
// connections it took are answered, so the caller can close the database.
func (s *Service) Serve(ctx context.Context, listener *net.UnixListener, peers peers) error {
	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	// Each connection has read and write deadlines, so waiting is bounded.
	var connections sync.WaitGroup
	defer connections.Wait()

	slots := map[Caller]chan struct{}{
		CallerDashboard: make(chan struct{}, maxConnections),
		CallerRoot:      make(chan struct{}, maxConnections),
	}
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var timeout net.Error
			if errors.As(err, &timeout) && timeout.Timeout() {
				continue
			}
			return err
		}

		// Identifying reads only what the kernel recorded, so it doesn't wait
		// on the caller.
		caller, err := peers.identify(conn)
		if err != nil {
			s.limits.countRefused()
			conn.Close()
			continue
		}

		select {
		case slots[caller] <- struct{}{}:
		default:
			conn.Close()
			continue
		}

		connections.Go(func() {
			defer func() { <-slots[caller] }()
			s.serveConn(ctx, conn, caller)
		})
	}
}

func (s *Service) serveConn(ctx context.Context, conn *net.UnixConn, caller Caller) {
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(idleTimeout))
	var req request
	if err := readMessage(conn, &req); err != nil {
		return
	}

	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	answer := s.handle(requestCtx, caller, req)

	conn.SetWriteDeadline(time.Now().Add(idleTimeout))
	writeMessage(conn, answer)
}

// maintain prunes every pruneInterval and flushes the dropped and refused
// counts every minute.
func (s *Service) maintain(ctx context.Context) {
	prune := time.NewTicker(pruneInterval)
	flush := time.NewTicker(flushInterval)
	defer prune.Stop()
	defer flush.Stop()

	if err := s.Prune(ctx); err != nil && ctx.Err() == nil {
		slog.Error("pruning failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-prune.C:
			if err := s.Prune(ctx); err != nil && ctx.Err() == nil {
				slog.Error("pruning failed", "error", err)
			}
		case <-flush.C:
			if err := s.FlushCounts(ctx); err != nil && ctx.Err() == nil {
				slog.Error("recording dropped events failed", "error", err)
			}
		}
	}
}
