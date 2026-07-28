import { createFibelApp } from "@k2b/fibel";

import config from "../fibel.config";

const siteUrl = config.siteUrl;
if (!siteUrl) {
  throw new Error("FIBEL_SITE_URL is required for the documentation check");
}

const app = await createFibelApp(config);
const checked = new Set<string>();
const queued = new Set<string>();
const queue = ["/robots.txt", "/sitemap.xml", "/llms.txt", "/llms-full.txt"];
const failures: string[] = [];

for (const path of queue) queued.add(path);

const sitemap = await request("/sitemap.xml");
if (!sitemap.ok) {
  throw new Error(`GET /sitemap.xml returned ${sitemap.status}`);
}
for (const match of (await sitemap.text()).matchAll(/<loc>([^<]+)<\/loc>/g)) {
  enqueue(match[1]);
}

const home = await request("/en");
const homeHtml = await home.text();
for (const expected of [
  'href="/en">Home</a>',
  'href="/en/getting-started">Install</a>',
  'href="/en/modules">Modules</a>',
  "data-fibel-mcp-open",
]) {
  if (!homeHtml.includes(expected)) {
    failures.push(`/en: missing ${expected}`);
  }
}
if (process.env.FIBEL_AI_MODEL && !homeHtml.includes("data-fibel-assistant-open")) {
  failures.push("/en: missing configured documentation assistant");
}

const mcp = await app.fetch(
  new Request(new URL("/_fibel/mcp", siteUrl), {
    method: "POST",
    headers: {
      Accept: "application/json, text/event-stream",
      "Content-Type": "application/json",
      Origin: new URL(siteUrl).origin,
    },
    body: JSON.stringify({
      jsonrpc: "2.0",
      id: 1,
      method: "initialize",
      params: {
        protocolVersion: "2025-03-26",
        capabilities: {},
        clientInfo: { name: "pulse-docs-check", version: "1.0.0" },
      },
    }),
  }),
);
if (!mcp.ok || !(await mcp.text()).includes('"serverInfo"')) {
  failures.push(`/_fibel/mcp: initialize returned HTTP ${mcp.status}`);
}

while (queue.length > 0) {
  const path = queue.shift()!;
  if (checked.has(path)) continue;
  checked.add(path);

  const response = await request(path);
  if (response.status >= 400) {
    failures.push(`${path}: HTTP ${response.status}`);
    continue;
  }

  const contentType = response.headers.get("content-type") ?? "";
  if (!contentType.includes("text/html")) continue;

  const html = await response.text();
  for (const match of html.matchAll(/href="([^"]+)"/g)) {
    enqueue(match[1], path);
  }
}

if (failures.length > 0) {
  throw new Error(`Documentation check failed:\n${failures.join("\n")}`);
}

console.log(`Checked ${checked.size} documentation routes.`);

function enqueue(value: string, from = "/") {
  if (
    value.startsWith("#") ||
    value.startsWith("mailto:") ||
    value.startsWith("tel:")
  ) {
    return;
  }

  const url = new URL(value, new URL(from, siteUrl));
  if (url.origin !== new URL(siteUrl).origin) return;
  url.hash = "";
  const path = `${url.pathname}${url.search}`;
  if (queued.has(path) || checked.has(path)) return;
  queued.add(path);
  queue.push(path);
}

function request(path: string) {
  return app.fetch(new Request(new URL(path, siteUrl)));
}
