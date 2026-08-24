import wails from "@wailsio/runtime/plugins/vite";
import { writeFileSync } from "node:fs";
import { defineConfig } from "vite";

// https://vitejs.dev/config/
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  plugins: [wails("./bindings"),{
    name: "keep-dist-placeholder",
    closeBundle: () => writeFileSync(new URL("./dist/.gitkeep", import.meta.url), ""),
  },],
});
