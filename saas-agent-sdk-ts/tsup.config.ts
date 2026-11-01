import { defineConfig } from "tsup";

export default defineConfig({
  entry: ["src/index.ts"],
  format: ["esm", "cjs"],
  outExtension({ format }) {
    return { js: format === "cjs" ? ".cjs" : ".js" };
  },
  dts: true,
  sourcemap: true,
  clean: true,
  target: "es2020",
  splitting: false,
  minify: false,
});
