import { promises as fs } from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

async function pathExists(target) {
  try {
    await fs.access(target);
    return true;
  } catch {
    return false;
  }
}

export function assetPaths(serverPath, cwd = process.cwd()) {
  const server = path.resolve(cwd, serverPath);
  const standaloneAppRoot = path.dirname(server);
  const appRoot = path.resolve(standaloneAppRoot, "..", "..", "..", "..");
  return {
    server,
    sourceStatic: path.join(appRoot, ".next", "static"),
    targetStatic: path.join(standaloneAppRoot, ".next", "static"),
    sourcePublic: path.join(appRoot, "public"),
    targetPublic: path.join(standaloneAppRoot, "public"),
  };
}

export async function stageAssets(paths) {
  if (!(await pathExists(paths.server))) {
    throw new Error(`standalone server 不存在：${paths.server}`);
  }
  if (!(await pathExists(paths.sourceStatic))) {
    throw new Error(`standalone 静态资源不存在，请先执行 build：${paths.sourceStatic}`);
  }
  await fs.cp(paths.sourceStatic, paths.targetStatic, { recursive: true, force: true });
  if (await pathExists(paths.sourcePublic)) {
    await fs.cp(paths.sourcePublic, paths.targetPublic, { recursive: true, force: true });
  }
}

export async function main(argv = process.argv.slice(2)) {
  const [serverPath, port] = argv;
  if (!serverPath) {
    throw new Error("Usage: node start-next-standalone.mjs <server-path> [port]");
  }
  if (!process.env.PORT && port) process.env.PORT = port;
  const paths = assetPaths(serverPath);
  await stageAssets(paths);
  await import(pathToFileURL(paths.server).href);
}

const entryPoint = process.argv[1] ? path.resolve(process.argv[1]) : "";
if (entryPoint === fileURLToPath(import.meta.url)) {
  try {
    await main();
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
