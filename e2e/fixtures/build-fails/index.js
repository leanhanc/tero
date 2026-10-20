require("node:http").createServer((_request, response) => response.end("unreachable\n")).listen(Number(process.env.PORT));
