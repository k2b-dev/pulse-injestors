import { createFibelApp } from "@k2b/fibel";

import config from "../fibel.config";

const app = await createFibelApp(config);

// Fibel builds absolute redirects from the request URL. Behind a TLS proxy
// that URL is plain http, so adopt the scheme the proxy received.
function withForwardedProto(request: Request) {
  if (request.headers.get("x-forwarded-proto") !== "https") return request;
  const url = new URL(request.url);
  if (url.protocol === "https:") return request;
  url.protocol = "https:";
  url.port = "";
  return new Request(url, request);
}

const server = Bun.serve({
  port: Number(process.env.PORT ?? 3000),
  fetch: (request) => app.fetch(withForwardedProto(request)),
});

for (const signal of ["SIGINT", "SIGTERM"] as const) {
  process.once(signal, async () => {
    await server.stop(true);
    process.exit(0);
  });
}
