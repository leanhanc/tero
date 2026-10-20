// A minimal backend for Tero's end-to-end tests. It answers with its
// fixture name, runtime and PORT so tests can check what Railpack built.
const http = require("node:http");
const ms = require("ms");

const runtime = typeof Bun === "undefined" ? `node ${process.version}` : `bun ${Bun.version}`;

http
  .createServer((_request, response) => {
    response.end(`tero fixture yarn on ${runtime}, port ${process.env.PORT}, ${ms(60000)}\n`);
  })
  .listen(Number(process.env.PORT));
