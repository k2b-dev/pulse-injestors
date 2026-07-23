import { defaultPlugins, defineFibel, type FibelPlugin } from "@valentinkolb/fibel";

const pulseHeaderLinks = (): FibelPlugin => ({
  name: "pulse-header-links",
  setup(context) {
    const renderPage = context.services.renderPage;
    context.services.renderPage = (page, request, currentContext) => {
      const localeRoot = `${currentContext.config.routing.basePath}/${page.locale.code}`;
      return renderPage(page, request, currentContext)
        .replace(`href="${localeRoot}/runtime"`, `href="${localeRoot}/getting-started"`)
        .replace(`href="${localeRoot}/plugins"`, `href="${localeRoot}/modules"`)
        .replace(">Docs Home</a>", ">Home</a>")
        .replace(">Guide</a>", ">Install</a>")
        .replace(">API Reference</a>", ">Modules</a>");
    };
  },
});

export default defineFibel({
  title: "Pulse Ingestors",
  description: "Documentation for Pulse telemetry ingestors.",
  siteUrl: "http://localhost:5173",
  locales: [{ code: "en", label: "English" }],
  defaultLocale: "en",
  routing: {
    basePath: "",
    internalPath: "/_fibel",
    assetsPath: "/assets",
  },
  plugins: [...defaultPlugins(), pulseHeaderLinks()],
});
