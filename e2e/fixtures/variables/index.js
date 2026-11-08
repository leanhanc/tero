// A backend for Tero's variable tests. It reports the E2E_ variables it runs
// with and which ones the build saw. E2E_CRASH makes it fail at start, printing
// a value on the way out to check it is hidden in the deploy log.
const fs = require("node:fs");
const http = require("node:http");

if (process.env.E2E_CRASH) {
  console.log(`e2e crashing with ${process.env.E2E_CRASH}`);
  process.exit(1);
}

const running = Object.fromEntries(Object.entries(process.env).filter(([name]) => name.startsWith("E2E_")));
const seenByBuild = JSON.parse(fs.readFileSync("build-seen.json", "utf8"));

http
  .createServer((_request, response) => {
    response.end(JSON.stringify({ pid: process.pid, running, seenByBuild }));
  })
  .listen(Number(process.env.PORT));
