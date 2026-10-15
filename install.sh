#!/bin/sh
# Installs the Tero binary.
#
#   curl -fsSL https://github.com/leanhanc/tero/releases/latest/download/install.sh | sh
#
# It checks the platform, downloads the release binary for this architecture,
# checks it against the release's checksums, and installs it as
# /usr/local/bin/tero. Configuring the server is a separate step: sudo tero init.
#
# TERO_DOWNLOAD_URL replaces the release download address, for example
# https://github.com/leanhanc/tero/releases/download/v0.1.0 to install a
# specific version. Downloads are HTTPS only.
set -eu

download_url="${TERO_DOWNLOAD_URL:-https://github.com/leanhanc/tero/releases/latest/download}"
install_path="/usr/local/bin/tero"
verify_docs="https://github.com/leanhanc/tero#verify-a-release"

# Everything runs from main, called on the last line, so a download cut short
# by the network never runs half a script.
main() {
	set_colors
	arch="$(release_arch)"
	check_platform "$arch"
	check_commands curl sha256sum

	work_dir="$(mktemp -d)"
	trap 'rm -rf "$work_dir"' EXIT

	binary="tero-linux-$arch"
	download "$binary" "$work_dir/$binary"
	download checksums.txt "$work_dir/checksums.txt"
	verify_checksum "$work_dir" "$binary"

	as_root install -m 0755 "$work_dir/$binary" "$install_path"

	version="$("$install_path" version)"
	printf '%s\n\n' "🎉 ${green}${bold}Tero $version is installed.${reset} To set up this server, run:"
	printf '%s\n' "   ${bold}sudo tero init${reset}"
}

# set_colors defines the styles for messages: color only on a terminal, and
# never when NO_COLOR is set (https://no-color.org).
set_colors() {
	bold="" green="" red="" reset=""
	if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
		bold="$(printf '\033[1m')"
		green="$(printf '\033[32m')"
		red="$(printf '\033[31m')"
		reset="$(printf '\033[0m')"
	fi
}

# release_arch maps uname -m to the architecture in release file names, or
# prints nothing for an architecture Tero has no release for.
release_arch() {
	case "$(uname -m)" in
	x86_64) echo amd64 ;;
	aarch64) echo arm64 ;;
	esac
}

# check_platform stops with the tested list unless this host is a tested
# Ubuntu release on a released architecture. It runs before anything is written.
check_platform() {
	arch="$1"
	os_id="$(os_release_field ID)"
	version_id="$(os_release_field VERSION_ID)"

	is_tested_release=false
	if [ "$os_id" = ubuntu ]; then
		case "$version_id" in
		24.04 | 26.04) is_tested_release=true ;;
		esac
	fi

	if [ "$is_tested_release" = true ] && [ -n "$arch" ]; then
		return
	fi

	os_name="$(os_release_field PRETTY_NAME)"
	fail "Tero hasn't been tested on ${os_name:-this OS} ($(uname -m)) yet, installation has been aborted.
Tested platforms:
  - Ubuntu 26.04 LTS on amd64 or arm64
  - Ubuntu 24.04 LTS on amd64 or arm64"
}

os_release_field() {
	if [ ! -r /etc/os-release ]; then
		return
	fi
	# os-release is a shell assignment list, read in a subshell so its
	# variables stay out of this script.
	(. /etc/os-release && eval "printf '%s' \"\${$1:-}\"")
}

check_commands() {
	for command in "$@"; do
		if ! command -v "$command" >/dev/null 2>&1; then
			fail "The install needs $command, which isn't on this server. Please install it and run the install again."
		fi
	done
}

download() {
	name="$1"
	destination="$2"
	if ! curl --proto '=https' --tlsv1.2 -fsSL --retry 3 -o "$destination" "$download_url/$name"; then
		fail "Couldn't download $name from $download_url. Please check the server's internet connection and run the install again."
	fi
}

verify_checksum() {
	dir="$1"
	binary="$2"
	expected="$(awk -v name="$binary" '$2 == name { print $1 }' "$dir/checksums.txt")"
	actual="$(sha256sum "$dir/$binary" | awk '{ print $1 }')"

	if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
		fail "The downloaded binary doesn't match the release checksum, so nothing was installed.
The download may be corrupted or tampered with. Please run the install again, or verify the release by hand:
$verify_docs"
	fi
}

as_root() {
	if [ "$(id -u)" -eq 0 ]; then
		"$@"
		return
	fi
	sudo "$@"
}

fail() {
	printf '%s\n' "❌ ${red}$1${reset}" >&2
	exit 1
}

main "$@"
