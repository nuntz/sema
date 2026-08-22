import { defineConfig, loadEnv } from "vite";
import solid from "vite-plugin-solid";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  return {
    plugins: [solid()],
    server: {
      proxy: { "/api": env.SEMA_API_ORIGIN || "http://localhost:8787" },
    },
    build: { target: "es2022" },
    test: { environment: "node" },
  };
});
