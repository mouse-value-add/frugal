#!/usr/bin/env node
// frugal-mcp — thin Node wrapper that fetches the matching Go binary
// from the GitHub release on first run, caches it under
// ~/.cache/frugal-mcp/<version>/, and execs it with the caller's argv.
//
// Why a runtime download instead of bundling binaries in the npm tarball
// (esbuild-style optionalDependencies)? Simpler to publish (one package,
// not five), works with `--ignore-scripts` (no postinstall), and the
// download is ~10 MB cached forever. The optionalDependencies layout is
// a drop-in upgrade later if the audience grows past the convenience
// audience this wrapper targets.

import { spawnSync } from "node:child_process";
import { createWriteStream, existsSync, mkdirSync, chmodSync, renameSync, unlinkSync, readFileSync } from "node:fs";
import { homedir, platform, arch } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { get } from "node:https";

const here = dirname(fileURLToPath(import.meta.url));
const pkg = JSON.parse(readFileSync(join(here, "..", "package.json"), "utf8"));

const VERSION = pkg.version;
const REPO = "brainsparker/frugal";

function targetTriple() {
  const os = platform();
  const cpu = arch();
  let goos;
  let goarch;
  switch (os) {
    case "darwin":
      goos = "darwin";
      break;
    case "linux":
      goos = "linux";
      break;
    default:
      // win32 is the other path most agent installs touch — Frugal has
      // no Windows release artifacts yet, so we fail fast with a useful
      // message rather than 404-ing on the download.
      throw new Error(`frugal-mcp: unsupported OS ${os} (darwin and linux only for now)`);
  }
  switch (cpu) {
    case "arm64":
      goarch = "arm64";
      break;
    case "x64":
      goarch = "amd64";
      break;
    default:
      throw new Error(`frugal-mcp: unsupported CPU ${cpu} (arm64 and x64 only)`);
  }
  return `${goos}-${goarch}`;
}

function cacheBinaryPath() {
  const cacheRoot = process.env.XDG_CACHE_HOME || join(homedir(), ".cache");
  return join(cacheRoot, "frugal-mcp", VERSION, `frugal-${targetTriple()}`);
}

function downloadFollowingRedirects(url, dest) {
  return new Promise((resolve, reject) => {
    const fetchOnce = (u, hops = 0) => {
      if (hops > 5) return reject(new Error("too many redirects"));
      const req = get(u, (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          res.resume();
          fetchOnce(new URL(res.headers.location, u).toString(), hops + 1);
          return;
        }
        if (res.statusCode !== 200) {
          res.resume();
          reject(new Error(`download ${u} → HTTP ${res.statusCode}`));
          return;
        }
        const out = createWriteStream(dest);
        res.pipe(out);
        out.on("finish", () => out.close(resolve));
        out.on("error", (err) => {
          try { unlinkSync(dest); } catch {}
          reject(err);
        });
      });
      req.on("error", reject);
    };
    fetchOnce(url);
  });
}

async function ensureBinary() {
  const binPath = cacheBinaryPath();
  if (existsSync(binPath)) return binPath;
  const triple = targetTriple();
  const url = `https://github.com/${REPO}/releases/download/v${VERSION}/frugal-${triple}`;
  mkdirSync(dirname(binPath), { recursive: true });
  // Download to a sibling temp path so a partial download from a crashed
  // process doesn't get cached as the canonical binary.
  const tmpPath = `${binPath}.partial`;
  process.stderr.write(`frugal-mcp: fetching v${VERSION} for ${triple} (~10 MB, cached after)…\n`);
  await downloadFollowingRedirects(url, tmpPath);
  chmodSync(tmpPath, 0o755);
  renameSync(tmpPath, binPath);
  return binPath;
}

async function main() {
  let binPath;
  try {
    binPath = await ensureBinary();
  } catch (err) {
    process.stderr.write(`frugal-mcp: ${err.message}\n`);
    process.exit(1);
  }
  const res = spawnSync(binPath, process.argv.slice(2), { stdio: "inherit" });
  if (res.error) {
    process.stderr.write(`frugal-mcp: exec failed: ${res.error.message}\n`);
    process.exit(1);
  }
  process.exit(res.status ?? 1);
}

main();
