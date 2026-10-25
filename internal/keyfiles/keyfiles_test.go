package keyfiles

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/store"
)

// fakeHost stands in for systemd: "sealing" writes the key as is, and
// restarting the key service starts a fresh one from the files in Dir.
type fakeHost struct {
	t        *testing.T
	root     string
	db       string
	client   *keys.Client
	commands []string
	// unreadable holds the contents of files that stop opening.
	unreadable map[string]bool
	// failRotate makes key.rotate fail.
	failRotate bool
}

func newFakeHost(t *testing.T) *fakeHost {
	host := &fakeHost{t: t, root: t.TempDir(), unreadable: map[string]bool{}}
	host.db = filepath.Join(t.TempDir(), "keys.db")
	host.client = keys.NewClient(filepath.Join(t.TempDir(), "down.sock"))
	return host
}

func (h *fakeHost) files() Files {
	return Files{
		Root: h.root,
		Run:  h,
		Seal: func(_ context.Context, _ string, key []byte, path string) error {
			return os.WriteFile(path, key, 0o600)
		},
		Opens: func(_ context.Context, _, path string) bool {
			contents, err := os.ReadFile(path)
			return err == nil && !h.unreadable[string(contents)]
		},
		Keys: h.client,
	}
}

func (h *fakeHost) Run(ctx context.Context, name string, args ...string) (string, error) {
	commandLine := strings.Join(append([]string{name}, args...), " ")
	h.commands = append(h.commands, commandLine)

	if commandLine == "systemctl restart tero-keys.service" || commandLine == "systemctl restart tero-keys.socket" {
		h.start()
	}
	if strings.HasPrefix(commandLine, "systemctl show tero-keys.service") {
		return "Result=exit-code\nExecMainStatus=243", nil
	}

	return "", nil
}

// start loads the keys the way systemd would, naming each file's credential
// kek_<file name>, and points the client at a new
// key service with them.
func (h *fakeHost) start() {
	h.t.Helper()
	credentials := h.t.TempDir()
	entries, _ := os.ReadDir(filepath.Join(h.root, Dir))
	for _, entry := range entries {
		contents, _ := os.ReadFile(filepath.Join(h.root, Dir, entry.Name()))
		os.WriteFile(filepath.Join(credentials, "kek_"+entry.Name()), contents, 0o400)
	}
	loaded, err := keys.LoadKeys(credentials)
	if err != nil {
		h.t.Fatal(err)
	}
	db, err := store.OpenWith(context.Background(), h.db, keys.Migrations)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { db.Close() })

	service, err := keys.NewService(db, loaded, keys.Options{})
	if err != nil {
		h.t.Fatal(err)
	}
	*h.client = *keys.NewLocalClient(service, keys.CallerRoot)
}

func TestSealNextAddsVersionsWithNothingLeftBehind(t *testing.T) {
	host := newFakeHost(t)
	files := host.files()
	ctx := context.Background()

	for want := 1; want <= 2; want++ {
		version, err := files.SealNext(ctx)
		if err != nil || version != want {
			t.Fatalf("SealNext = %d, %v; want %d", version, err, want)
		}
	}

	versions, _ := files.Versions()
	if !slices.Equal(versions, []int{1, 2}) {
		t.Errorf("versions = %v", versions)
	}
	staged, _ := os.ReadDir(filepath.Join(host.root, newDir))
	if len(staged) != 0 {
		t.Errorf("staging directory still holds %d files", len(staged))
	}
	info, _ := os.Stat(filepath.Join(host.root, Dir, "1"))
	if info.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %o", info.Mode().Perm())
	}
}

func TestSealNextRefusesAKeyThatDoesntOpen(t *testing.T) {
	host := newFakeHost(t)
	files := host.files()
	files.Seal = func(_ context.Context, _ string, _ []byte, path string) error {
		host.unreadable["broken"] = true
		return os.WriteFile(path, []byte("broken"), 0o600)
	}

	if _, err := files.SealNext(context.Background()); err == nil {
		t.Fatal("a key that doesn't open was moved into place")
	}
	if versions, _ := host.files().Versions(); len(versions) != 0 {
		t.Errorf("versions = %v", versions)
	}
}

// key-service: Rotation
func TestRotateLeavesOnlyTheNewKey(t *testing.T) {
	host := newFakeHost(t)
	files := host.files()
	ctx := context.Background()
	files.SealNext(ctx)
	host.start()
	if _, err := host.client.IssueSetupLink(ctx); err != nil {
		t.Fatal(err)
	}

	newest, err := files.Rotate(ctx)
	if err != nil {
		t.Fatal(err)
	}

	versions, _ := files.Versions()
	if newest != 2 || !slices.Equal(versions, []int{2}) {
		t.Errorf("after rotation: newest %d, versions %v", newest, versions)
	}
	if !slices.Contains(host.commands, "systemctl restart tero-keys.service") {
		t.Error("the key service wasn't restarted")
	}
	for _, command := range host.commands {
		if strings.Contains(command, "tero.service") {
			t.Errorf("rotation touched the dashboard: %s", command)
		}
	}
}

// key-service: Interrupted rotation
func TestFailedRotationKeepsTheOldKey(t *testing.T) {
	host := newFakeHost(t)
	files := host.files()
	ctx := context.Background()
	files.SealNext(ctx)
	host.start()

	// The new key is sealed, then the key service goes away mid-rotation.
	host.client = keys.NewClient(filepath.Join(t.TempDir(), "down.sock"))
	files.Keys = host.client
	files.Run = &sysexecStub{}
	if _, err := files.Rotate(ctx); err == nil {
		t.Fatal("rotation succeeded without the key service")
	}

	versions, _ := files.Versions()
	if !slices.Equal(versions, []int{1, 2}) {
		t.Errorf("versions after a failed rotation = %v, want both kept", versions)
	}
}

type sysexecStub struct{}

func (sysexecStub) Run(context.Context, string, ...string) (string, error) { return "", nil }

// dashboard-access: Master key lost
func TestRecoverSetsAsideUnreadableKeys(t *testing.T) {
	host := newFakeHost(t)
	files := host.files()
	ctx := context.Background()
	files.SealNext(ctx)
	old, _ := os.ReadFile(filepath.Join(host.root, Dir, "1"))
	host.unreadable[string(old)] = true

	replaced, err := files.Recover(ctx)
	if err != nil || !replaced {
		t.Fatalf("Recover = %v, %v", replaced, err)
	}

	versions, _ := files.Versions()
	// The new key takes a new number, so nothing sealed with the lost one
	// can be mistaken for it.
	if !slices.Equal(versions, []int{2}) {
		t.Fatalf("versions = %v, want only a new 2", versions)
	}
	matches, _ := filepath.Glob(filepath.Join(host.root, UnreadableDir, "*", "1"))
	if len(matches) != 1 {
		t.Fatalf("set-aside files = %v", matches)
	}
	kept, _ := os.ReadFile(matches[0])
	fresh, _ := os.ReadFile(filepath.Join(host.root, Dir, "2"))
	if !bytes.Equal(kept, old) || bytes.Equal(fresh, old) {
		t.Error("the unreadable key wasn't kept, or wasn't replaced")
	}
	if _, err := host.client.Status(ctx); err != nil {
		t.Errorf("key service after recovery: %v", err)
	}
}

func TestRecoverLeavesARunningKeyServiceAlone(t *testing.T) {
	host := newFakeHost(t)
	files := host.files()
	ctx := context.Background()
	files.SealNext(ctx)
	host.start()
	host.commands = nil

	if replaced, err := files.Recover(ctx); err != nil || replaced {
		t.Fatalf("Recover = %v, %v", replaced, err)
	}
	if len(host.commands) != 0 {
		t.Errorf("ran %v", host.commands)
	}
}

func TestRecoverRefusesOtherFailures(t *testing.T) {
	host := newFakeHost(t)
	files := host.files()
	files.Run = &showResult{output: "Result=signal\nExecMainStatus=0"}

	if _, err := files.Recover(context.Background()); err == nil || errors.Is(err, keys.ErrUnavailable) {
		t.Errorf("Recover = %v, want a refusal that points at the journal", err)
	}
}

type showResult struct{ output string }

func (s *showResult) Run(context.Context, string, ...string) (string, error) { return s.output, nil }
