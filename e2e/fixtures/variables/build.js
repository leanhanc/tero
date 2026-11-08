// Records which variables the build saw, without their values, so the image
// holds none. It prints one on purpose, to check it is hidden in the build log.
const fs = require("node:fs");

const seen = Object.keys(process.env).filter((name) => name.startsWith("E2E_"));
fs.writeFileSync("build-seen.json", JSON.stringify(seen));
console.log(`e2e printing ${process.env.E2E_PRINTED_IN_BUILD}`);
