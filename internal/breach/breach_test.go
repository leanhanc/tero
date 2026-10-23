package breach

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/leanhanc/tero/internal/keyfiles"
	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/store"
	"github.com/leanhanc/tero/internal/totp"
)

// fakeServer stands in for systemd and the key service.
type fakeServer struct {
	t        *testing.T
	root     string
	dbPath   string
	client   *keys.Client
	service  *keys.Service
	commands []string
	// dashboardUp is whether tero.service is running.
	dashboardUp bool
	// upDuringReset records whether the dashboard ran while the login was
	// reset.
	upDuringReset bool
	hasTPM        bool
}

func newFakeServer(t *testing.T) *fakeServer {
	server := &fakeServer{t: t, root: t.TempDir(), dashboardUp: true}
	server.dbPath = filepath.Join(t.TempDir(), "keys.db")
	server.client = &keys.Client{}
	return server
}

func (s *fakeServer) Run(_ context.Context, name string, args ...string) (string, error) {
	commandLine := strings.Join(append([]string{name}, args...), " ")
	s.commands = append(s.commands, commandLine)

	switch commandLine {
	case "systemctl stop tero.service":
		s.dashboardUp = false
	case "systemctl start tero.service":
		s.dashboardUp = true
	case "systemctl restart tero-keys.service":
		s.startKeys()
	case "systemd-creds has-tpm2 --quiet":
		if !s.hasTPM {
			return "", errors.New("no TPM")
		}
	case "pgrep --uid 999":
		return "", noMatch{}
	}

	return "", nil
}

// noMatch is pgrep's exit status 1.
type noMatch struct{}

func (noMatch) Error() string { return "exit status 1" }
func (noMatch) ExitCode() int { return 1 }

func (s *fakeServer) startKeys() {
	// systemd names each file's credential kek_<file name>.
	credentials := s.t.TempDir()
	entries, _ := os.ReadDir(filepath.Join(s.root, keyfiles.Dir))
	for _, entry := range entries {
		contents, _ := os.ReadFile(filepath.Join(s.root, keyfiles.Dir, entry.Name()))
		os.WriteFile(filepath.Join(credentials, "kek_"+entry.Name()), contents, 0o400)
	}
	loaded, err := keys.LoadKeys(credentials)
	if err != nil {
		s.t.Fatal(err)
	}
	db, err := store.OpenWith(context.Background(), s.dbPath, keys.Migrations)
	if err != nil {
		s.t.Fatal(err)
	}
	s.t.Cleanup(func() { db.Close() })

	s.service = keys.NewService(db, loaded, keys.Options{})
	*s.client = *keys.NewLocalClient(s.service, keys.CallerRoot)
}

func (s *fakeServer) files() keyfiles.Files {
	return keyfiles.Files{
		Root: s.root,
		Run:  s,
		Seal: func(_ context.Context, _ string, key []byte, path string) error {
			return os.WriteFile(path, key, 0o600)
		},
		Opens: func(context.Context, string, string) bool { return true },
		Keys:  s.client,
	}
}

func (s *fakeServer) options(out *bytes.Buffer, answer string) Options {
	return Options{
		Domain:  "dash.example.com",
		In:      strings.NewReader(answer),
		Out:     out,
		Root:    s.root,
		Run:     s,
		Keys:    s.files(),
		TeroUID: 999,
		ClearSessions: func(context.Context) error {
			s.commands = append(s.commands, "clear sessions")
			return nil
		},
	}
}

// setUp gives the fake server a master key, an admin and certificates.
func (s *fakeServer) setUp(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	files := s.files()
	if _, err := files.SealNext(ctx); err != nil {
		t.Fatal(err)
	}
	s.startKeys()

	token, _ := s.client.IssueSetupLink(ctx)
	dashboard := keys.NewLocalClient(s.service, keys.CallerDashboard)
	enrolment, err := dashboard.BeginSetup(ctx, token, "a long passphrase nobody has used")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.Code(enrolment.Secret, time.Now())
	if err := dashboard.FinishSetup(ctx, "", token, code); err != nil {
		t.Fatal(err)
	}

	os.MkdirAll(filepath.Join(s.root, caddyStorage, "certificates"), 0o700)
	os.WriteFile(filepath.Join(s.root, caddyStorage, "certificates", "key.pem"), []byte("key"), 0o600)
}

// breach-response: Confirmed run, Nothing survives in the dashboard's memory
func TestAssumeBreachResetsEverything(t *testing.T) {
	server := newFakeServer(t)
	server.setUp(t)
	var out bytes.Buffer
	ctx := context.Background()

	if err := Run(ctx, server.options(&out, "yes\n")); err != nil {
		t.Fatal(err)
	}

	stopAt := slices.Index(server.commands, "systemctl stop tero.service")
	stopUserAt := slices.Index(server.commands, "systemctl stop user-999.slice user-runtime-dir@999.service")
	killAt := slices.Index(server.commands, "pkill --signal KILL --uid 999")
	clearAt := slices.Index(server.commands, "clear sessions")
	startUserAt := slices.Index(server.commands, "systemctl start user@999.service")
	startAt := slices.Index(server.commands, "systemctl start tero.service")
	isInOrder := stopAt >= 0 && stopUserAt > stopAt && killAt > stopUserAt && clearAt > killAt &&
		startUserAt > clearAt && startAt > startUserAt
	if !isInOrder || !server.dashboardUp {
		t.Errorf("commands in the wrong order: %v", server.commands)
	}

	status, err := server.client.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.IsSetUp || !slices.Equal(status.KeyVersions, []int{2}) {
		t.Errorf("after the reset: %+v", status)
	}
	if _, err := os.Stat(filepath.Join(server.root, caddyStorage)); !os.IsNotExist(err) {
		t.Error("certificates and keys were kept")
	}

	events, _ := server.client.RecentEvents(ctx, 10)
	if events[0].Type != keys.EventBreachReset || events[0].Source != "root" {
		t.Errorf("latest event = %+v", events[0])
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); !strings.HasPrefix(last, "https://dash.example.com/setup#") {
		t.Errorf("last line = %q", last)
	}
}

// breach-response: Downtime said up front, Not confirmed
func TestAssumeBreachAsksFirst(t *testing.T) {
	server := newFakeServer(t)
	server.setUp(t)
	var out bytes.Buffer

	err := Run(context.Background(), server.options(&out, "y\n"))
	if !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("Run = %v", err)
	}
	if !strings.Contains(out.String(), "apps are down") {
		t.Errorf("the prompt doesn't mention downtime:\n%s", out.String())
	}
	if slices.Contains(server.commands, "systemctl stop tero.service") {
		t.Error("the dashboard was stopped without confirmation")
	}
}

// breach-response: Scripted run
func TestAssumeBreachWithYesDoesntAsk(t *testing.T) {
	server := newFakeServer(t)
	server.setUp(t)
	var out bytes.Buffer
	opts := server.options(&out, "")
	opts.IsConfirmed = true

	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Type yes") {
		t.Error("asked despite --yes")
	}
}

const userUnits = teroHome + "/.config/systemd/user"

// breach-response: What to do elsewhere, Server without a TPM
func TestAssumeBreachReportsWhatToCheck(t *testing.T) {
	server := newFakeServer(t)
	server.setUp(t)
	os.MkdirAll(filepath.Join(server.root, userUnits), 0o700)
	os.WriteFile(filepath.Join(server.root, userUnits, "miner.service"), []byte("[Service]"), 0o600)
	var out bytes.Buffer

	if err := Run(context.Background(), server.options(&out, "yes\n")); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"latest Tero", "authenticator app", "authorized_keys", userUnits, "no TPM"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output is missing %q:\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(filepath.Join(server.root, userUnits, "miner.service")); !os.IsNotExist(err) {
		t.Error("the planted unit was left in place")
	}
	moved, _ := filepath.Glob(filepath.Join(server.root, QuarantineDir, "*", userUnits, "miner.service"))
	if len(moved) != 1 {
		t.Errorf("the planted unit wasn't kept in %s: %v", QuarantineDir, moved)
	}

	server.hasTPM = true
	out.Reset()
	Run(context.Background(), server.options(&out, "yes\n"))
	if strings.Contains(out.String(), "no TPM") {
		t.Error("the TPM warning shows on a server with a TPM")
	}
}

func TestDashboardStartsAgainWhenAStepFails(t *testing.T) {
	server := newFakeServer(t)
	server.setUp(t)
	var out bytes.Buffer
	opts := server.options(&out, "yes\n")
	opts.ClearSessions = func(context.Context) error { return errors.New("database locked") }

	if err := Run(context.Background(), opts); err == nil {
		t.Fatal("Run succeeded")
	}
	if !server.dashboardUp {
		t.Error("the dashboard stayed stopped after a failure")
	}
}

func TestPlantedLinkMovesOnlyTheLink(t *testing.T) {
	server := newFakeServer(t)
	server.setUp(t)
	elsewhere := filepath.Join(server.root, "root/.ssh")
	os.MkdirAll(elsewhere, 0o700)
	os.WriteFile(filepath.Join(elsewhere, "authorized_keys"), []byte("ssh-ed25519 AAAA"), 0o600)
	os.MkdirAll(filepath.Join(server.root, teroHome, ".config/systemd"), 0o700)
	os.Symlink(elsewhere, filepath.Join(server.root, userUnits))
	// A file where a directory belongs doesn't stop the reset.
	os.MkdirAll(filepath.Join(server.root, teroHome, ".local"), 0o700)
	os.WriteFile(filepath.Join(server.root, teroHome, ".local/share"), []byte("x"), 0o600)
	var out bytes.Buffer

	if err := Run(context.Background(), server.options(&out, "yes\n")); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(elsewhere, "authorized_keys")); err != nil {
		t.Errorf("the link's target was moved: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(server.root, userUnits)); !os.IsNotExist(err) {
		t.Error("the planted link was left in place")
	}
	for _, want := range []string{userUnits, teroHome + "/.local/share", "/var/lib/tero-breach/"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output is missing %q:\n%s", want, out.String())
		}
	}
}

func TestCrontabMovedBeforeTheKill(t *testing.T) {
	server := newFakeServer(t)
	server.setUp(t)
	os.MkdirAll(filepath.Join(server.root, "var/spool/cron/crontabs"), 0o700)
	os.WriteFile(filepath.Join(server.root, teroCrontab), []byte("* * * * * true"), 0o600)
	var out bytes.Buffer
	opts := server.options(&out, "yes\n")
	run := opts.Run
	var isMovedAtKill bool
	opts.Run = runnerFunc(func(ctx context.Context, name string, args ...string) (string, error) {
		if name == "pkill" {
			_, err := os.Stat(filepath.Join(server.root, teroCrontab))
			isMovedAtKill = os.IsNotExist(err)
		}
		return run.Run(ctx, name, args...)
	})

	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if !isMovedAtKill {
		t.Error("the crontab was still in place when tero's processes were killed")
	}
}

func TestLeftoverProcessFailsTheRun(t *testing.T) {
	server := newFakeServer(t)
	server.setUp(t)
	var out bytes.Buffer
	opts := server.options(&out, "yes\n")
	run := opts.Run
	opts.Run = runnerFunc(func(ctx context.Context, name string, args ...string) (string, error) {
		if name == "pgrep" {
			return "4242", nil
		}
		return run.Run(ctx, name, args...)
	})

	if err := Run(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "didn't stop") {
		t.Fatalf("Run = %v", err)
	}
	if status, _ := server.client.Status(context.Background()); !status.IsSetUp {
		t.Error("the login was reset with a tero process still running")
	}
}

type runnerFunc func(ctx context.Context, name string, args ...string) (string, error)

func (f runnerFunc) Run(ctx context.Context, name string, args ...string) (string, error) {
	return f(ctx, name, args...)
}
