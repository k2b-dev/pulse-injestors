import { createFibelApp } from "@k2b/fibel";

import config from "../fibel.config";

const app = await createFibelApp(config);
const server = Bun.serve({
  port: Number(process.env.PORT ?? 3000),
  fetch: app.fetch,
});

for (const signal of ["SIGINT", "SIGTERM"] as const) {
  process.once(signal, async () => {
    await server.stop(true);
    process.exit(0);
  });
}
