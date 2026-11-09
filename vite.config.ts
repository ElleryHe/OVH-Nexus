import { defineConfig } from "vite";
import react from "@vitejs/plugin-react-swc";
import path from "path";
import fs from "fs";

const pkg = JSON.parse(
  fs.readFileSync(path.resolve(__dirname, "./package.json"), "utf-8")
);

// https://vitejs.dev/config/
export default defineConfig({
  define: {
    __APP_VERSION__: JSON.stringify(pkg.version || "1.0.0"),
  },
  server: {
    host: "127.0.0.1",
    port: 19999,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:19998",
        changeOrigin: false,
        xfwd: true,
        configure: (proxy) => {
          proxy.on("proxyReq", (proxyReq, req) => {
            const remoteIp = req.socket?.remoteAddress || "127.0.0.1";
            proxyReq.setHeader("X-Real-IP", remoteIp);
          });
        },
      },
    },
  },
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
});
