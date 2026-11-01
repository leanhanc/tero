#!/bin/bash
# Runs the end-to-end suite on both tested Ubuntu releases and keeps the
# output in ~/e2e.log, for the amd64 machine that can't be driven remotely.
#
#   git pull && scripts/e2e-amd64.sh
set -euo pipefail

cd "$(dirname "$0")/.."
git log --oneline -1
scripts/vm-test.sh 2>&1 | tee ~/e2e.log
