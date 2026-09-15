import { mkdtemp, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";

// Run against the exact Cloud checkout intended for the next deployment.
const cloud = process.env.PULSE_CLOUD_CHECKOUT;
if (!cloud) throw new Error("Set PULSE_CLOUD_CHECKOUT to the target Cloud checkout (with dependencies installed)");
const root = resolve(import.meta.dir, "..");
const source = resolve(cloud, "packages/pulse/src");
const { IngestBatchSchema } = await import(pathToFileURL(join(source, "api/schemas.ts")).href);
const { compileDashboardDsl } = await import(pathToFileURL(join(source, "dashboard-dsl/compiler.ts")).href);
const { compilePulseQueryText } = await import(pathToFileURL(join(source, "query-dsl/index.ts")).href);
const output = await mkdtemp(join(tmpdir(), "pulse-contract-"));
const run = async (args: string[]) => {
  const proc = Bun.spawn(args, {
    cwd: root,
    env: { ...process.env, PULSE_CONTRACT_OUTPUT: output },
    stdout: "inherit", stderr: "inherit",
  });
  if (await proc.exited !== 0) throw new Error(`Contract command failed: ${args.join(" ")}`);
};
try {
  await run(["go", "test", "./internal/modules/...", "-run", "^TestPulseContractExport$", "-count=1"]);
  await run(["go", "test", "./internal/pulse", "-run", "^TestPulseContractWire$", "-count=1"]);
  const files = (await readdir(join(output, "wire"))).filter(name => name.endsWith(".json"));
  if (files.length === 0) throw new Error("No collector payloads were exported");
  for (const file of files) {
    const batch = await Bun.file(join(output, "wire", file)).json();
    const result = IngestBatchSchema.safeParse(batch);
    if (!result.success) throw new Error(`${file}: ${JSON.stringify(result.error.issues)}`);
    console.log(`Accepted ${file}: ${batch.metrics?.length ?? 0} metrics, ${batch.events?.length ?? 0} events, ${batch.states?.length ?? 0} states`);
  }
  for (const file of (await readdir(join(root, "examples/dashboards"))).filter(name => name.endsWith(".pulse"))) {
    const result = compileDashboardDsl(await Bun.file(join(root, "examples/dashboards", file)).text(), (query: string) => {
      const compiled = compilePulseQueryText("contract-check", query);
      return compiled.ok ? compiled : { ok: false, message: compiled.error.message };
    });
    if (!result.ok) throw new Error(`${file}: ${JSON.stringify(result.diagnostics)}`);
    console.log(`Compiled ${file}`);
  }
  console.log(`Pulse contract verified against ${resolve(cloud)}`);
} finally {
  await rm(output, { recursive: true, force: true });
}
