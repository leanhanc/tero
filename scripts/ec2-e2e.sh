#!/bin/bash
# Runs the end-to-end suite on throwaway EC2 servers, one per architecture and
# Ubuntu release, in parallel. For each it copies Canonical's current image
# with NitroTPM turned on (Canonical's images have it off, and the suite checks
# that the master key is sealed with the TPM), launches a server from it, runs
# scripts/ec2-test.sh and terminates the server. Everything a run creates is
# tagged tero-e2e=<run id> and removed on exit, failed or interrupted runs
# included. Logs go to .tero-e2e/<run id>/<arch>-<release>.log.
#
#   scripts/ec2-e2e.sh                 every architecture and release
#   scripts/ec2-e2e.sh arm64           both releases on arm64
#   scripts/ec2-e2e.sh amd64 24.04     one server
#   scripts/ec2-e2e.sh all 26.04       one release on every architecture
#   scripts/ec2-e2e.sh cleanup         remove everything tagged tero-e2e
#
# Uses the AWS profile in AWS_PROFILE (default tero) and its Region.
# TERO_E2E_AMD64_TYPE and TERO_E2E_ARM64_TYPE pick the instance types. The
# defaults, t3.small and t4g.small (2 GiB each), are the smallest the suite
# passes on in about 35 minutes.
set -euo pipefail

export AWS_PROFILE="${AWS_PROFILE:-tero}" AWS_PAGER=""
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
canonical_owner="099720109477"
root_volume_gib=20
all_archs=(amd64 arm64)
all_releases=(26.04 24.04)

main() {
	cd "$repo_root"
	if [[ "${1:-}" == "cleanup" ]]; then
		remove_tagged ""
		return
	fi

	local archs=("${all_archs[@]}") releases=("${all_releases[@]}")
	if [[ -n "${1:-}" && "$1" != "all" ]]; then
		archs=("$1")
	fi
	if [[ -n "${2:-}" ]]; then
		releases=("$2")
	fi

	run_id="$(date -u +%Y%m%d-%H%M%S)-$RANDOM"
	run_dir="$repo_root/.tero-e2e/$run_id"
	mkdir -p "$run_dir"
	trap 'finish $?' EXIT
	trap 'exit 130' INT TERM
	echo "=== Run $run_id in $(aws configure get region), logs in .tero-e2e/$run_id"

	git log --oneline -1
	create_access
	local arch release
	for arch in "${archs[@]}"; do
		echo "=== Building for $arch"
		scripts/ec2-test.sh build "$arch" >"$run_dir/build-$arch.log" 2>&1 ||
			fail "build for $arch failed, see .tero-e2e/$run_id/build-$arch.log"
	done

	for arch in "${archs[@]}"; do
		for release in "${releases[@]}"; do
			test_on_server "$arch" "$release" &
		done
	done
	wait
	report
}

fail() {
	echo "=== $*" >&2
	exit 1
}

# create_access makes the run's key pair and a security group that lets SSH
# in from this machine only, plus HTTP and HTTPS, in the default VPC.
create_access() {
	local my_ip vpc_id
	my_ip="$(curl -fsS https://checkip.amazonaws.com)"
	vpc_id="$(aws ec2 describe-vpcs --filters Name=is-default,Values=true \
		--query 'Vpcs[0].VpcId' --output text)"

	key_name="tero-e2e-$run_id"
	key_file="$run_dir/key.pem"
	aws ec2 create-key-pair --key-name "$key_name" --key-type ed25519 \
		--tag-specifications "ResourceType=key-pair,Tags=[$(tags)]" \
		--query KeyMaterial --output text >"$key_file"
	chmod 600 "$key_file"

	group_id="$(aws ec2 create-security-group --group-name "tero-e2e-$run_id" \
		--description "Tero e2e run $run_id" --vpc-id "$vpc_id" \
		--tag-specifications "ResourceType=security-group,Tags=[$(tags)]" \
		--query GroupId --output text)"
	aws ec2 authorize-security-group-ingress --group-id "$group_id" --ip-permissions \
		"IpProtocol=tcp,FromPort=22,ToPort=22,IpRanges=[{CidrIp=$my_ip/32}]" \
		"IpProtocol=tcp,FromPort=80,ToPort=80,IpRanges=[{CidrIp=0.0.0.0/0}]" \
		"IpProtocol=tcp,FromPort=443,ToPort=443,IpRanges=[{CidrIp=0.0.0.0/0}]" >/dev/null
}

tags() {
	echo "{Key=tero-e2e,Value=$run_id},{Key=Name,Value=tero-e2e-$run_id${1:+-$1}}"
}

# test_on_server runs the suite on a new server for one architecture and
# release, writes passed or failed to <name>.result and the seconds the server
# ran to <name>.seconds.
test_on_server() {
	local arch="$1" release="$2"
	local name="$arch-$release"
	local log="$run_dir/$name.log"
	local image_id instance_id ip started result=failed

	say "$name" "preparing an image with NitroTPM"
	if image_id="$(prepare_image "$arch" "$release" "$name" 2>>"$log")"; then
		say "$name" "launching $(instance_type "$arch")"
		instance_id="$(launch "$arch" "$image_id" "$name" 2>>"$log")" || instance_id=""
	fi

	if [[ -n "${instance_id:-}" ]]; then
		started="$(date +%s)"
		ip="$(wait_for_ssh "$instance_id" 2>>"$log")" || ip=""
		if [[ -n "$ip" ]]; then
			say "$name" "running the suite on $ip"
			if TERO_E2E_PREBUILT=1 scripts/ec2-test.sh "ubuntu@$ip" "$key_file" >>"$log" 2>&1; then
				result=passed
			fi
			ssh-keygen -R "$ip" >/dev/null 2>&1 || true
		fi
		aws ec2 terminate-instances --instance-ids "$instance_id" >/dev/null
		aws ec2 wait instance-terminated --instance-ids "$instance_id"
		echo $(($(date +%s) - started)) >"$run_dir/$name.seconds"
	fi

	echo "$result" >"$run_dir/$name.result"
	say "$name" "$result"
}

say() {
	echo "=== $1: $2"
}

instance_type() {
	if [[ "$1" == "amd64" ]]; then
		echo "${TERO_E2E_AMD64_TYPE:-t3.small}"
		return
	fi
	echo "${TERO_E2E_ARM64_TYPE:-t4g.small}"
}

# prepare_image copies Canonical's newest image for the release and registers
# its snapshot again as a UEFI image with NitroTPM, then drops the copy. Only
# the registered image and its snapshot stay, until the run ends.
prepare_image() {
	local arch="$1" release="$2" name="$3"
	local source_id copy_id snapshot_id aws_arch
	aws_arch="$arch"
	if [[ "$arch" == "amd64" ]]; then
		aws_arch="x86_64"
	fi

	source_id="$(aws ec2 describe-images --owners "$canonical_owner" \
		--filters "Name=name,Values=ubuntu/images/hvm-ssd*/ubuntu-*-$release-$arch-server-*" \
		"Name=architecture,Values=$aws_arch" "Name=state,Values=available" \
		--query 'sort_by(Images,&CreationDate)[-1].ImageId' --output text)"
	if [[ "$source_id" == "None" ]]; then
		echo "no Canonical image for Ubuntu $release on $arch" >&2
		return 1
	fi
	echo "Canonical image $source_id" >&2

	copy_id="$(aws ec2 copy-image --source-region "$(aws configure get region)" \
		--source-image-id "$source_id" --name "tero-e2e-$run_id-$name-copy" \
		--tag-specifications "ResourceType=image,Tags=[$(tags "$name")]" \
		"ResourceType=snapshot,Tags=[$(tags "$name")]" \
		--query ImageId --output text)"
	wait_for_image "$copy_id"
	snapshot_id="$(aws ec2 describe-images --image-ids "$copy_id" \
		--query 'Images[0].BlockDeviceMappings[0].Ebs.SnapshotId' --output text)"

	aws ec2 register-image --name "tero-e2e-$run_id-$name" --architecture "$aws_arch" \
		--root-device-name /dev/sda1 --virtualization-type hvm --ena-support \
		--boot-mode uefi --tpm-support v2.0 --imds-support v2.0 \
		--block-device-mappings "DeviceName=/dev/sda1,Ebs={SnapshotId=$snapshot_id,VolumeSize=$root_volume_gib,VolumeType=gp3,DeleteOnTermination=true}" \
		--tag-specifications "ResourceType=image,Tags=[$(tags "$name")]" \
		--query ImageId --output text
	aws ec2 deregister-image --image-id "$copy_id" >/dev/null
}

# wait_for_image polls instead of using aws ec2 wait, which gives up after ten
# minutes, shorter than a copy sometimes takes.
wait_for_image() {
	local image_id="$1"
	local state
	for _ in $(seq 120); do
		state="$(aws ec2 describe-images --image-ids "$image_id" --query 'Images[0].State' --output text)"
		case "$state" in
		available) return 0 ;;
		failed | invalid | error) echo "image $image_id is $state" >&2 && return 1 ;;
		esac
		sleep 15
	done
	echo "image $image_id still $state after 30 minutes" >&2
	return 1
}

launch() {
	local arch="$1" image_id="$2" name="$3"
	aws ec2 run-instances --image-id "$image_id" --instance-type "$(instance_type "$arch")" \
		--key-name "$key_name" --security-group-ids "$group_id" --count 1 \
		--metadata-options HttpTokens=required \
		--tag-specifications "ResourceType=instance,Tags=[$(tags "$name")]" \
		"ResourceType=volume,Tags=[$(tags "$name")]" \
		--query 'Instances[0].InstanceId' --output text
}

# wait_for_ssh waits until the server accepts the run's key and prints its
# public IP.
wait_for_ssh() {
	local instance_id="$1"
	local ip
	aws ec2 wait instance-running --instance-ids "$instance_id"
	ip="$(aws ec2 describe-instances --instance-ids "$instance_id" \
		--query 'Reservations[0].Instances[0].PublicIpAddress' --output text)"
	ssh-keygen -R "$ip" >/dev/null 2>&1 || true
	for _ in $(seq 60); do
		if ssh -i "$key_file" -o BatchMode=yes -o StrictHostKeyChecking=accept-new \
			-o ConnectTimeout=5 "ubuntu@$ip" true 2>/dev/null; then
			echo "$ip"
			return 0
		fi
		sleep 5
	done
	echo "no SSH on $ip after 5 minutes" >&2
	return 1
}

# report prints which servers passed and failed and the instance hours the
# run used, and fails when any server didn't pass.
report() {
	local passed=() failed=() seconds=0 file name
	for file in "$run_dir"/*.result; do
		name="$(basename "$file" .result)"
		if [[ "$(cat "$file")" == "passed" ]]; then
			passed+=("$name")
		else
			failed+=("$name")
		fi
	done
	for file in "$run_dir"/*.seconds; do
		[[ -e "$file" ]] && seconds=$((seconds + $(cat "$file")))
	done
	echo "=== Passed: ${passed[*]:-none}"
	echo "=== Failed: ${failed[*]:-none}"
	echo "=== Instance hours: $(awk "BEGIN { printf \"%.2f\", $seconds / 3600 }")"
	[[ ${#failed[@]} -eq 0 ]]
}

finish() {
	local status="$1"
	trap - EXIT INT TERM
	# Background servers still running after an interrupt stop here first.
	kill $(jobs -p) 2>/dev/null || true
	wait 2>/dev/null || true
	echo "=== Removing what run $run_id created"
	remove_tagged "$run_id"
	rm -f "$run_dir/key.pem"
	exit "$status"
}

# remove_tagged removes what is tagged tero-e2e with the given run id, or with
# any run id when it is empty, in the order that frees dependencies first.
remove_tagged() {
	local run="$1"
	local filter="Name=tag-key,Values=tero-e2e"
	if [[ -n "$run" ]]; then
		filter="Name=tag:tero-e2e,Values=$run"
	fi
	local instances images snapshots keys groups id

	instances="$(aws ec2 describe-instances --filters "$filter" \
		"Name=instance-state-name,Values=pending,running,stopping,stopped,shutting-down" \
		--query 'Reservations[].Instances[].InstanceId' --output text)"
	if [[ -n "$instances" ]]; then
		echo "terminating $instances"
		aws ec2 terminate-instances --instance-ids $instances >/dev/null
		aws ec2 wait instance-terminated --instance-ids $instances
	fi

	images="$(aws ec2 describe-images --owners self --filters "$filter" --query 'Images[].ImageId' --output text)"
	for id in $images; do
		echo "deregistering $id"
		aws ec2 deregister-image --image-id "$id" >/dev/null
	done

	# A copy still in progress keeps its snapshot busy; it is removed by a
	# later cleanup.
	snapshots="$(aws ec2 describe-snapshots --owner-ids self --filters "$filter" \
		--query 'Snapshots[].SnapshotId' --output text)"
	for id in $snapshots; do
		echo "deleting $id"
		aws ec2 delete-snapshot --snapshot-id "$id" || true
	done

	keys="$(aws ec2 describe-key-pairs --filters "$filter" --query 'KeyPairs[].KeyPairId' --output text)"
	for id in $keys; do
		echo "deleting key pair $id"
		aws ec2 delete-key-pair --key-pair-id "$id" >/dev/null
	done

	groups="$(aws ec2 describe-security-groups --filters "$filter" --query 'SecurityGroups[].GroupId' --output text)"
	for id in $groups; do
		echo "deleting security group $id"
		aws ec2 delete-security-group --group-id "$id" >/dev/null
	done
}

# Exit on the same line: bash reads a script as it runs, so a run started
# before an edit to this file would otherwise read past main into the new text.
main "$@"; exit
