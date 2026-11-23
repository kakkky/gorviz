// client のコードを esbuild で dist/ に書き出す。--test は単体テストを node_modules/gorviz-test-dist/ に書き出す
import { build } from "esbuild"
import { readdirSync } from "node:fs"

const test = process.argv.includes("--test")
const controllers = readdirSync("src/controllers").filter((f) => f.endsWith("_controller.ts")).map((f) => `src/controllers/${f}`)

if (test) {
  const tests = readdirSync("src/gmp_replay").filter((f) => f.endsWith(".test.ts")).map((f) => `src/gmp_replay/${f}`)
  await build({ entryPoints: tests, outdir: "node_modules/gorviz-test-dist", bundle: true, format: "esm", platform: "node", outExtension: { ".js": ".mjs" }, logLevel: "warning" })
} else {
  const common = { bundle: true, format: "esm", target: "es2022", minify: true, legalComments: "none", logLevel: "info", external: ["@hotwired/stimulus"], jsx: "automatic", jsxImportSource: "preact" }
  await build({ ...common, entryPoints: [...controllers, "src/islands.ts"], outdir: "dist/js" })
  await build({ entryPoints: ["src/main.css"], outfile: "dist/main.css", bundle: true, minify: true, logLevel: "info" })
}
