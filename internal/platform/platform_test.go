package platform

import (
	"strings"
	"testing"
)

const ubuntu2604 = `PRETTY_NAME="Ubuntu 26.04 LTS"
NAME="Ubuntu"
VERSION_ID="26.04"
ID=ubuntu
ID_LIKE=debian
`

const ubuntu2404 = `PRETTY_NAME="Ubuntu 24.04.3 LTS"
VERSION_ID="24.04"
ID=ubuntu
`

const ubuntu2204 = `PRETTY_NAME="Ubuntu 22.04.5 LTS"
VERSION_ID="22.04"
ID=ubuntu
`

func TestIsTested(t *testing.T) {
	cases := []struct {
		name      string
		osRelease string
		arch      string
		want      bool
	}{
		{"ubuntu 26.04 arm64", ubuntu2604, "arm64", true},
		{"ubuntu 26.04 amd64", ubuntu2604, "amd64", true},
		{"ubuntu 26.04 riscv64", ubuntu2604, "riscv64", false},
		{"ubuntu 24.04 arm64", ubuntu2404, "arm64", true},
		{"ubuntu 24.04 amd64", ubuntu2404, "amd64", true},
		{"ubuntu 22.04 arm64", ubuntu2204, "arm64", false},
		{"ubuntu 22.04 amd64", ubuntu2204, "amd64", false},
		{"debian", "ID=debian\nVERSION_ID=\"13\"\n", "amd64", false},
		{"empty os-release", "", "amd64", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.osRelease, tc.arch).IsTested()
			if got != tc.want {
				t.Fatalf("IsTested() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestString(t *testing.T) {
	got := Parse(ubuntu2404, "arm64").String()
	if got != "Ubuntu 24.04.3 LTS (arm64)" {
		t.Fatalf("String() = %q", got)
	}
}

func TestTestedListNamesEveryUbuntuRelease(t *testing.T) {
	for _, want := range []string{"Ubuntu 26.04 LTS on amd64 or arm64", "Ubuntu 24.04 LTS on amd64 or arm64"} {
		if !strings.Contains(TestedList(), want) {
			t.Errorf("TestedList() = %q, missing %q", TestedList(), want)
		}
	}
}
