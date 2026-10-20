// Nothing tells Railpack to run this file: there is no start script, no
// main field and no index.js, so the deploy must fail with advice.
require("node:http").createServer((_request, response) => response.end("unreachable\n")).listen(Number(process.env.PORT));
