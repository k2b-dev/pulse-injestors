import { defaultPlugins, defineFibel } from "@k2b/fibel";
import {
  assistantPlugin,
  imprintPlugin,
  mcpPlugin,
  providerFromEnv,
} from "@k2b/fibel/plugins";

const assistantPlugins = process.env.FIBEL_AI_MODEL?.trim()
  ? [
      assistantPlugin({
        provider: providerFromEnv(),
        launcherLabel: "Ask Pulse Docs",
        systemPrompt:
          "Help administrators install, configure, and troubleshoot Pulse Ingestors. Base answers on the documentation and keep commands precise.",
      }),
    ]
  : [];

export default defineFibel({
  title: "Pulse Ingestors",
  description: "Install, configure, and operate Pulse telemetry ingestors for infrastructure monitoring.",
  siteUrl: process.env.FIBEL_SITE_URL,
  locales: [{ code: "en", label: "English" }],
  defaultLocale: "en",
  headerLinks: [
    { label: "Home", value: "/" },
    { label: "Install", value: "/getting-started" },
    { label: "Modules", value: "/modules" },
  ],
  seo: {
    disallow: [],
  },
  routing: {
    basePath: "",
    internalPath: "/_fibel",
    assetsPath: "/assets",
  },
  plugins: [
    ...defaultPlugins(),
    ...assistantPlugins,
    mcpPlugin(),
    imprintPlugin({ url: "https://impressum.valentin-kolb.com" }),
  ],
});
