// A minimal backend for Tero's end-to-end tests, run by Bun.
import ms from "ms";

Bun.serve({
  port: Number(process.env.PORT),
  fetch() {
    return new Response(`tero fixture bun on bun ${Bun.version}, port ${process.env.PORT}, ${ms(60000)}\n`);
  },
});
