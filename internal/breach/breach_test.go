package breach

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
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
	// session is the dashboard session setup started.
	session string
	// failing is a command line that fails.
	failing string
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
	if commandLine == s.failing {
		return "", errors.New("failed")
	}

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
	case "pgrep --uid 999", "pgrep --uid 998":
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

	s.service, err = keys.NewService(db, loaded, keys.Options{})
	if err != nil {
		s.t.Fatal(err)
	}
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
		Domain:   "dash.example.com",
		In:       strings.NewReader(answer),
		Out:      out,
		Root:     s.root,
		Run:      s,
		Keys:     s.files(),
		TeroUID:  999,
		AgentUID: 998,
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
	enrolment, err := dashboard.BeginSetup(ctx, "", token, "a long passphrase nobody has used")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.Code(enrolment.Secret, time.Now())
	login, err := dashboard.FinishSetup(ctx, keys.Browser{}, token, code)
	if err != nil {
		t.Fatal(err)
	}
	s.session = login.SessionToken

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
	stopTeroAt := slices.Index(server.commands, "systemctl stop user-999.slice user-runtime-dir@999.service")
	stopAgentAt := slices.Index(server.commands, "systemctl stop user-998.slice user-runtime-dir@998.service")
	killTeroAt := slices.Index(server.commands, "pkill --signal KILL --uid 999")
	killAgentAt := slices.Index(server.commands, "pkill --signal KILL --uid 998")
	// The rotation restarts the key service after the login is reset.
	clearAt := slices.Index(server.commands, "systemctl restart tero-keys.service")
	startAgentAt := slices.Index(server.commands, "systemctl start user@998.service")
	startAt := slices.Index(server.commands, "systemctl start tero.service")
	clearCacheAt := slices.IndexFunc(server.commands, func(command string) bool {
		return strings.HasPrefix(command, "systemd-run --machine=tero-agent@.host --user --wait --quiet podman volume rm")
	})
	isInOrder := clearCacheAt > slices.Index(server.commands, "systemctl start user@998.service") &&
		stopAt >= 0 && stopTeroAt > stopAt && stopAgentAt > stopAt &&
		killTeroAt > stopAgentAt && killAgentAt > stopAgentAt &&
		clearAt > killTeroAt && clearAt > killAgentAt && startAgentAt > clearAt && startAt > startAgentAt
	if !isInOrder || !server.dashboardUp {
		t.Errorf("commands in the wrong order: %v", server.commands)
	}
	if slices.Contains(server.commands, "systemctl start user@999.service") {
		t.Error("tero's user manager was started; tero has none")
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

	dashboard := keys.NewLocalClient(server.service, keys.CallerDashboard)
	if _, err := dashboard.CheckSession(ctx, server.session, ""); !errors.Is(err, keys.ErrSessionEnded) {
		t.Errorf("session after the reset = %v", err)
	}
	if !strings.Contains(out.String(), "passkeys") {
		t.Errorf("the output doesn't mention passkeys:\n%s", out.String())
	}

	events, _ := server.client.RecentEvents(ctx, "", 10)
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

const (
	userUnits      = teroHome + "/.config/systemd/user"
	agentUserUnits = agentHome + "/.config/systemd/user"
	teroCrontab    = crontabDir + "/tero"
	agentCrontab   = crontabDir + "/tero-agent"
)

// breach-response: What to do elsewhere, Server without a TPM
func TestAssumeBreachReportsWhatToCheck(t *testing.T) {
	server := newFakeServer(t)
	server.setUp(t)
	os.MkdirAll(filepath.Join(server.root, userUnits), 0o700)
	os.WriteFile(filepath.Join(server.root, userUnits, "miner.service"), []byte("[Service]"), 0o600)
	os.MkdirAll(filepath.Join(server.root, agentUserUnits), 0o700)
	os.WriteFile(filepath.Join(server.root, agentUserUnits, "backdoor.service"), []byte("[Service]"), 0o600)
	var out bytes.Buffer

	if err := Run(context.Background(), server.options(&out, "yes\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(server.root, agentUserUnits, "backdoor.service")); !os.IsNotExist(err) {
		t.Error("the unit planted for tero-agent was left in place")
	}

	for _, want := range []string{"latest Tero", "authenticator app", "authorized_keys", "Redeploy every app", teroHome + "/.config", agentHome + "/.config", "no TPM"} {
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
	server.failing = "systemctl restart tero-keys.service"

	if err := Run(context.Background(), server.options(&out, "yes\n")); err == nil {
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
	for _, want := range []string{teroHome + "/.config", teroHome + "/.local/share", "/var/lib/tero-breach/"} {
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
	os.WriteFile(filepath.Join(server.root, agentCrontab), []byte("* * * * * true"), 0o600)
	var out bytes.Buffer
	opts := server.options(&out, "yes\n")
	run := opts.Run
	isMovedAtKill := true
	opts.Run = runnerFunc(func(ctx context.Context, name string, args ...string) (string, error) {
		if name == "pkill" {
			_, teroErr := os.Stat(filepath.Join(server.root, teroCrontab))
			_, agentErr := os.Stat(filepath.Join(server.root, agentCrontab))
			isMovedAtKill = isMovedAtKill && os.IsNotExist(teroErr) && os.IsNotExist(agentErr)
		}
		return run.Run(ctx, name, args...)
	})

	if err := Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if !isMovedAtKill {
		t.Error("a crontab was still in place when the processes were killed")
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

// breach-response: GitHub connection deleted, What to do elsewhere
func TestAssumeBreachDeletesTheGitHubConnection(t *testing.T) {
	server := newFakeServer(t)
	server.setUp(t)
	ctx := context.Background()
	dashboard := keys.NewLocalClient(server.service, keys.CallerDashboard)
	agent := keys.NewLocalClient(server.service, keys.CallerAgent)
	state, err := dashboard.BeginGitHubConnection(ctx, server.session)
	if err != nil {
		t.Fatal(err)
	}
	dashboard.ApproveGitHubConnection(ctx, server.session, state)
	err = agent.ConnectGitHub(ctx, keys.GitHubAppSecrets{
		State: state, AppID: 7, Slug: "tero-dash", Owner: "acme", IsOrgOwned: true,
		PrivateKey: testAppKey, WebhookSecret: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer

	if err := Run(ctx, server.options(&out, "yes\n")); err != nil {
		t.Fatal(err)
	}

	app, _ := server.client.GitHubStatus(ctx)
	if app.IsConnected {
		t.Error("GitHub is still connected after assume-breach")
	}
	if !strings.Contains(out.String(), "Delete the GitHub App tero-dash at https://github.com/organizations/acme/settings/apps/tero-dash") {
		t.Errorf("the output doesn't say to delete the App:\n%s", out.String())
	}
}

var testAppKey = func() string {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}()
