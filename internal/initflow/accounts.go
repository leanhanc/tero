package initflow

import (
	"strconv"
	"strings"

	"github.com/leanhanc/tero/internal/hostfs"
)

type account struct {
	name  string
	uid   int
	gid   int
	home  string
	shell string
}

// readAccounts parses /etc/passwd.
func readAccounts(files hostfs.FS) (map[string]account, error) {
	data, err := files.ReadFile("/etc/passwd")
	if err != nil {
		return nil, err
	}

	accounts := map[string]account{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) < 7 {
			continue
		}

		uid, uidErr := strconv.Atoi(fields[2])
		gid, gidErr := strconv.Atoi(fields[3])
		if uidErr != nil || gidErr != nil {
			continue
		}

		accounts[fields[0]] = account{name: fields[0], uid: uid, gid: gid, home: fields[5], shell: fields[6]}
	}

	return accounts, nil
}

type group struct {
	gid     int
	members []string
}

// readGroups parses /etc/group.
func readGroups(files hostfs.FS) (map[string]group, error) {
	data, err := files.ReadFile("/etc/group")
	if err != nil {
		return nil, err
	}

	groups := map[string]group{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) < 4 {
			continue
		}

		gid, err := strconv.Atoi(fields[2])
		if err != nil {
			continue
		}

		var members []string
		if fields[3] != "" {
			members = strings.Split(fields[3], ",")
		}
		groups[fields[0]] = group{gid: gid, members: members}
	}

	return groups, nil
}
