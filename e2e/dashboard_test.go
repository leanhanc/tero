//go:build e2e

package e2e

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/leanhanc/tero/internal/totp"
)

// setupLink is the link init printed; testInit sets it.
var setupLink string

// adminPassword is 64 lowercase letters, fresh for every run so it is in no
// breach list.
var adminPassword = randomLowercase(64)

const breachedPassword = "passwordpassword"

// dashboard holds what the admin knows while walking through the dashboard.
type dashboard struct {
	token    string
	secret   string
	lastStep int64
}

func testDashboardAccess(t *testing.T) {
	admin := &dashboard{token: tokenOf(t, setupLink)}

	t.Run("dashboard-access: No login before setup", admin.testNoLoginBeforeSetup)
	t.Run("dashboard-access: Password too short (v5.0.0-6.2.1)", admin.testPasswordTooShort)
	t.Run("dashboard-access: Breached password (v5.0.0-6.2.12)", admin.testBreachedPassword)
	t.Run("dashboard-access: Long password accepted (v5.0.0-6.2.9)", admin.testLongPasswordAccepted)
	t.Run("dashboard-access: Setup without TOTP", admin.testSetupWithoutTOTP)
	t.Run("dashboard-access: Admin completes setup / Cookie attributes / Session cookie flags", admin.testCompleteSetup)
	if t.Failed() {
		t.FailNow()
	}

	t.Run("dashboard-access: Link reused / Second account", admin.testLinkReused)
	t.Run("dashboard-access: Stored form", testStoredForm)
	t.Run("dashboard-access: Correct password and code", admin.testCorrectLogin)
	t.Run("dashboard-access: Replayed code (v5.0.0-6.5.1)", admin.testReplayedCode)
	t.Run("dashboard-access: Correct password, wrong code", admin.testWrongCode)
	t.Run("dashboard-access: Events recorded / No secrets in events", admin.testEventsRecorded)
	t.Run("process-isolation: Dashboard cannot trigger root work", admin.testNoRootWork)
	t.Run("dashboard-access: Repeated failures from one IP (v5.0.0-6.3.1)", admin.testRateLimit)
	t.Run("dashboard-access: Admin loses their authenticator / Credential change revokes sessions / Server settings unaffected by reset", admin.testResetLogin)
	t.Run("dashboard-access: Recovery visible after the fact", admin.testRecoveryVisible)
}

func (d *dashboard) testNoLoginBeforeSetup(t *testing.T) {
	response := postDashboard(t, "anonymous", "/api/login", map[string]string{"password": adminPassword, "code": "123456"})
	if response.status != 409 {
		t.Errorf("login before setup = %d", response.status)
	}
	assertContains(t, response.body, "setup link")
}

func (d *dashboard) testPasswordTooShort(t *testing.T) {
	response := postDashboard(t, "anonymous", "/api/setup/begin", map[string]string{"token": d.token, "password": "fourteen chars"})
	if response.status != 422 {
		t.Errorf("14-character password = %d", response.status)
	}
	assertContains(t, response.body, "15 characters")
}

func (d *dashboard) testBreachedPassword(t *testing.T) {
	response := postDashboard(t, "anonymous", "/api/setup/begin", map[string]string{"token": d.token, "password": breachedPassword})
	if response.status != 422 {
		t.Errorf("breached password = %d", response.status)
	}
	assertContains(t, response.body, "known from data breaches")
}

func (d *dashboard) testLongPasswordAccepted(t *testing.T) {
	response := postDashboard(t, "anonymous", "/api/setup/begin", map[string]string{"token": d.token, "password": adminPassword})
	if response.status != 200 {
		t.Fatalf("64-character password = %d %s", response.status, response.body)
	}

	var enrolment struct{ Secret, URI string }
	if err := json.Unmarshal([]byte(response.body), &enrolment); err != nil {
		t.Fatal(err)
	}
	assertContains(t, enrolment.URI, "otpauth://totp/Tero:admin@"+skippedDomain)
	d.secret = enrolment.Secret
}

func (d *dashboard) testSetupWithoutTOTP(t *testing.T) {
	response := postDashboard(t, "anonymous", "/api/setup/finish", map[string]string{"token": d.token, "code": "000000"})
	if response.status != 422 {
		t.Errorf("finish with a wrong code = %d", response.status)
	}

	assertContains(t, getDashboard(t, "anonymous", "/api/state").body, `"isSetUp":false`)
}

func (d *dashboard) testCompleteSetup(t *testing.T) {
	response := postDashboard(t, "admin", "/api/setup/finish", map[string]string{"token": d.token, "code": d.nextCode(t)})
	if response.status != 204 {
		t.Fatalf("finish setup = %d %s", response.status, response.body)
	}

	cookie := setCookieHeader(response.headers)
	for _, attribute := range []string{"__Host-tero_session=", "Path=/", "Secure", "HttpOnly", "SameSite=Strict"} {
		assertContains(t, cookie, attribute)
	}
	if strings.Contains(strings.ToLower(cookie), "domain=") {
		t.Errorf("session cookie has a Domain attribute: %s", cookie)
	}

	assertContains(t, getDashboard(t, "admin", "/api/state").body, `"isSetUp":true`, `"isLoggedIn":true`)
}

func (d *dashboard) testLinkReused(t *testing.T) {
	response := postDashboard(t, "anonymous", "/api/setup/begin", map[string]string{"token": d.token, "password": randomLowercase(40)})
	if response.status != 403 {
		t.Errorf("reusing the setup link = %d", response.status)
	}
}

func testStoredForm(t *testing.T) {
	if got := must(t, "sudo stat -c '%a %U' /var/lib/tero/tero.db"); got != "600 tero" {
		t.Errorf("database is %q, want 600 tero", got)
	}

	databaseFiles := "sudo cat /var/lib/tero/tero.db /var/lib/tero/tero.db-wal 2>/dev/null"
	plaintextCount := run(t, databaseFiles+" | grep -a -c "+adminPassword)
	if strings.TrimSpace(plaintextCount.output) != "0" {
		t.Errorf("the database holds the plaintext password (%s matches)", strings.TrimSpace(plaintextCount.output))
	}
	must(t, databaseFiles+` | grep -a -q '\$argon2id\$v=19\$m=65536,t=3,p=4\$'`)
}

func (d *dashboard) testCorrectLogin(t *testing.T) {
	response := postDashboard(t, "second-browser", "/api/login", map[string]string{"password": adminPassword, "code": d.nextCode(t)})
	if response.status != 204 {
		t.Fatalf("login = %d %s", response.status, response.body)
	}
	assertContains(t, getDashboard(t, "second-browser", "/api/state").body, `"isLoggedIn":true`)
}

func (d *dashboard) testReplayedCode(t *testing.T) {
	code := d.nextCode(t)
	first := postDashboard(t, "replay", "/api/login", map[string]string{"password": adminPassword, "code": code})
	if first.status != 204 {
		t.Fatalf("first login = %d %s", first.status, first.body)
	}

	replay := postDashboard(t, "replay", "/api/login", map[string]string{"password": adminPassword, "code": code})
	if replay.status != 401 {
		t.Errorf("replayed code = %d", replay.status)
	}
}

func (d *dashboard) testWrongCode(t *testing.T) {
	wrongCode := postDashboard(t, "anonymous", "/api/login", map[string]string{"password": adminPassword, "code": "000000"})
	wrongPassword := postDashboard(t, "anonymous", "/api/login", map[string]string{"password": randomLowercase(30), "code": "000000"})

	if wrongCode.status != 401 || wrongCode.body != wrongPassword.body {
		t.Errorf("wrong code = %d %s, wrong password = %s; want the same generic answer", wrongCode.status, wrongCode.body, wrongPassword.body)
	}
}

func (d *dashboard) testEventsRecorded(t *testing.T) {
	events := getDashboard(t, "admin", "/api/events")
	if events.status != 200 {
		t.Fatalf("events = %d %s", events.status, events.body)
	}
	assertContains(t, events.body, "setup_link_issued", "setup_completed", "login_succeeded", "login_failed", `"ip":"127.0.0.1"`)

	journal := must(t, "sudo journalctl SYSLOG_IDENTIFIER=tero --no-pager -o export | grep -a '^TERO_EVENT=' | sort -u")
	assertContains(t, journal, "TERO_EVENT=setup_completed", "TERO_EVENT=login_succeeded", "TERO_EVENT=login_failed")

	allJournal := must(t, "sudo journalctl SYSLOG_IDENTIFIER=tero --no-pager -o export")
	for _, secret := range []string{adminPassword, d.secret, cookieValue(t, "admin")} {
		if strings.Contains(events.body, secret) || strings.Contains(allJournal, secret) {
			t.Error("a security event contains a secret")
		}
	}
}

// testNoRootWork audits every process the service starts while each
// dashboard route is used. The service must start none at all, so none can
// be root.
func (d *dashboard) testNoRootWork(t *testing.T) {
	servicePID := must(t, "systemctl show tero --property=MainPID --value")
	must(t, "sudo auditctl -a always,exit -F arch=b64 -S execve,execveat -F ppid="+servicePID+" -k e2e-tero-exec")
	defer run(t, "sudo auditctl -d always,exit -F arch=b64 -S execve,execveat -F ppid="+servicePID+" -k e2e-tero-exec")

	getDashboard(t, "admin", "/")
	getDashboard(t, "admin", "/api/state")
	getDashboard(t, "admin", "/api/events")
	postDashboard(t, "throwaway", "/api/login", map[string]string{"password": randomLowercase(30), "code": "000000"})
	postDashboard(t, "throwaway", "/api/setup/begin", map[string]string{"token": d.token, "password": randomLowercase(30)})
	postDashboard(t, "throwaway", "/api/setup/finish", map[string]string{"token": d.token, "code": "000000"})
	postDashboard(t, "throwaway", "/api/logout", map[string]string{})

	spawned := run(t, "sudo ausearch -k e2e-tero-exec --raw")
	if strings.Contains(spawned.output, "type=SYSCALL") {
		t.Errorf("the service started processes:\n%s", spawned.output)
	}
}

// testRateLimit gets a valid code before failing, because the first lockout
// lasts only 30 seconds and waiting for a fresh code could outlast it.
func (d *dashboard) testRateLimit(t *testing.T) {
	validCode := d.nextCode(t)

	var response dashboardResponse
	for range 6 {
		response = postDashboard(t, "attacker", "/api/login", map[string]string{"password": randomLowercase(30), "code": "000000"})
	}
	if response.status != 429 {
		t.Fatalf("login after 5 failures = %d", response.status)
	}

	rightCredentials := postDashboard(t, "attacker", "/api/login", map[string]string{"password": adminPassword, "code": validCode})
	if rightCredentials.status != 429 || rightCredentials.body != response.body {
		t.Errorf("rate-limited login with the right credentials = %d %s", rightCredentials.status, rightCredentials.body)
	}

	must(t, "sudo systemctl restart tero")
	waitUntil(t, time.Minute, func() bool { return getDashboard(t, "anonymous", "/api/state").status == 200 })

	afterRestart := postDashboard(t, "attacker", "/api/login", map[string]string{"password": randomLowercase(30), "code": "000000"})
	if afterRestart.status != 429 {
		t.Errorf("rate limit after a service restart = %d", afterRestart.status)
	}
}

func (d *dashboard) testResetLogin(t *testing.T) {
	before := etcChecksum(t)

	got := run(t, "sudo tero reset-login")
	if got.exitCode != 0 {
		t.Fatalf("reset-login failed (%d):\n%s", got.exitCode, got.output)
	}
	newLink := lastLine(got.output)
	if !strings.HasPrefix(newLink, "https://"+skippedDomain+"/setup#") {
		t.Fatalf("reset-login did not print a setup link: %q", got.output)
	}

	if after := etcChecksum(t); after != before {
		t.Error("reset-login changed files under /etc")
	}
	assertServesHTTPS(t, skippedDomain)

	if response := getDashboard(t, "admin", "/api/events"); response.status != 401 {
		t.Errorf("old session after reset = %d", response.status)
	}
	oldCredentials := postDashboard(t, "anonymous", "/api/login", map[string]string{"password": adminPassword, "code": d.nextCode(t)})
	if oldCredentials.status != 409 {
		t.Errorf("old credentials after reset = %d", oldCredentials.status)
	}

	d.token = tokenOf(t, newLink)
	d.lastStep = 0
}

func (d *dashboard) testRecoveryVisible(t *testing.T) {
	begin := postDashboard(t, "admin", "/api/setup/begin", map[string]string{"token": d.token, "password": adminPassword})
	if begin.status != 200 {
		t.Fatalf("setup after reset = %d %s", begin.status, begin.body)
	}
	var enrolment struct{ Secret string }
	json.Unmarshal([]byte(begin.body), &enrolment)
	d.secret = enrolment.Secret

	finish := postDashboard(t, "admin", "/api/setup/finish", map[string]string{"token": d.token, "code": d.nextCode(t)})
	if finish.status != 204 {
		t.Fatalf("finish setup after reset = %d %s", finish.status, finish.body)
	}

	assertContains(t, getDashboard(t, "admin", "/api/events").body, "login_reset")
}

// nextCode waits for a TOTP step the server hasn't accepted yet and returns
// its code, acting as the admin's authenticator app.
func (d *dashboard) nextCode(t *testing.T) string {
	t.Helper()

	for totp.Step(time.Now()) <= d.lastStep {
		time.Sleep(time.Second)
	}
	d.lastStep = totp.Step(time.Now())

	code, err := totp.Code(d.secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return code
}

type dashboardResponse struct {
	status  int
	headers string
	body    string
}

// postDashboard sends JSON to the dashboard from inside the VM, keeping
// cookies per browser name, like a browser on the dashboard's own origin.
func postDashboard(t *testing.T, browser, path string, body any) dashboardResponse {
	t.Helper()

	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.StdEncoding.EncodeToString(encoded)

	return requestDashboard(t, browser, fmt.Sprintf(
		`echo %s | base64 -d | curl -X POST -H 'Content-Type: application/json' -H 'Origin: https://%s' --data-binary @- `,
		payload, skippedDomain), path)
}

func getDashboard(t *testing.T, browser, path string) dashboardResponse {
	t.Helper()
	return requestDashboard(t, browser, "curl ", path)
}

// requestDashboard runs curl in the VM. The output is the body, a status
// marker line, then the response headers. A connection failure is status 0.
func requestDashboard(t *testing.T, browser, curlPrefix, path string) dashboardResponse {
	t.Helper()

	jar := "/tmp/e2e-cookies-" + browser
	script := fmt.Sprintf(`%s -sS -k --resolve %s:443:127.0.0.1 -b %s -c %s -D /tmp/e2e-headers -w '\n%s%%{http_code}\n' https://%s%s && cat /tmp/e2e-headers`,
		curlPrefix, skippedDomain, jar, jar, statusMarker, skippedDomain, path)
	got := run(t, script)
	if got.exitCode != 0 {
		return dashboardResponse{}
	}

	body, rest, _ := strings.Cut(got.output, "\n"+statusMarker)
	statusLine, headers, _ := strings.Cut(rest, "\n")
	status, _ := strconv.Atoi(strings.TrimSpace(statusLine))

	return dashboardResponse{status: status, body: strings.TrimSpace(body), headers: headers}
}

const statusMarker = "__E2E_STATUS__"

func setCookieHeader(headers string) string {
	for _, line := range strings.Split(headers, "\n") {
		if strings.HasPrefix(strings.ToLower(line), "set-cookie:") {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func cookieValue(t *testing.T, browser string) string {
	t.Helper()
	jar := must(t, "cat /tmp/e2e-cookies-"+browser)
	for _, line := range strings.Split(jar, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 7 && fields[5] == "__Host-tero_session" {
			return fields[6]
		}
	}
	t.Fatalf("no session cookie for %s", browser)
	return ""
}

func tokenOf(t *testing.T, link string) string {
	t.Helper()
	_, token, isFound := strings.Cut(link, "#")
	if !isFound || token == "" {
		t.Fatalf("no token in setup link %q", link)
	}
	return token
}

func randomLowercase(length int) string {
	random := make([]byte, length)
	rand.Read(random)
	for index := range random {
		random[index] = 'a' + random[index]%26
	}
	return string(random)
}
