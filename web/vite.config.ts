import { execFileSync } from "node:child_process";
import { defineConfig, loadEnv } from "vite";
import solid from "vite-plugin-solid";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const builtAt = new Date().toISOString();
  let build: string;
  try {
    build = execFileSync("git", ["rev-parse", "--short", "HEAD"], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
  } catch {
    build = String(Date.now());
  }
  return {
    define: {
      __SEMA_BUILD__: JSON.stringify(build),
      __SEMA_BUILT_AT__: JSON.stringify(builtAt),
    },
    plugins: [
      solid(),
      {
        name: "sema-version",
        apply: "build",
        generateBundle() {
          this.emitFile({
            type: "asset",
            fileName: "version.json",
            source: JSON.stringify({ build, builtAt }),
          });
        },
      },
      {
        name: "reader-media-fixtures",
        apply: "serve",
        configureServer(server) {
          server.middlewares.use((request, _response, next) => {
            if (request.url?.startsWith("/media/e2e/")) {
              request.url = request.url.slice("/media".length);
            }
            next();
          });
        },
      },
    ],
    server: {
      proxy: { "/api": env.SEMA_API_ORIGIN || "http://localhost:8787" },
    },
    build: { target: "es2022" },
    test: { environment: "node" },
  };
});
