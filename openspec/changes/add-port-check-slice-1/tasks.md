## 1. Port check

- [x] 1.1 Add `internal/portcheck` with the connect-back client (IPv4 only, answer must match the public IP and port, 10 s timeout, 4 KiB body); unit tests with an httptest server cover open, closed, wrong address, wrong port, bad JSON and an unreachable service
- [x] 1.2 Detect the provider from `/sys/class/dmi/id`; unit tests cover AWS, Google Cloud, Azure, Oracle Cloud and unknown
- [x] 1.3 Write the copy and the report, with color only on a terminal and without `NO_COLOR`; unit tests cover `server-init: Both ports open`, `Check unavailable` and `No color off a terminal`
- [x] 1.4 Run the check in init after the Tero service starts, skip the certificate wait when both ports are closed, and keep the link last
- [x] 1.5 Add `e2e/fakeprobe` and `TERO_E2E_PORT_PROBE` (e2e builds only), copy it in `scripts/ec2-test.sh`, and cover `server-init: Port closed at the provider` in the init e2e test
- [x] 1.6 Write `docs/ports.md` and link it from the README and the init output
- [x] 1.7 Tick the slice 1 roadmap items
