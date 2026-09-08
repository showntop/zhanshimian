#!/usr/bin/env node
/**
 * 契约同步检查：contracts/openapi.yaml 的 paths 与 @zsm/core 路径常量双向对齐。
 *
 * 用法：node contracts/scripts/check-sync.mjs
 * 退出码：0 = 一致（或 core 未就绪跳过）；1 = 存在差异 / 文件异常。
 *
 * 零依赖约束：不引入 yaml 解析库，用正则从 openapi.yaml 提取路径与方法。
 * 对 YAML / TS 结构的健壮性假设（如格式变化请同步维护本节与 openapi.yaml 头部注释）：
 *   1. `paths:` 顶格独占一行；其下的路径键缩进恰好 2 个空格（可带单/双引号），
 *      形如 `  '/v1/tasks/{id}':`；`paths:` 段在下一个顶格键（如 `components:`）处结束；
 *   2. 每个路径项下的 HTTP 方法键（get/post/put/patch/delete/options/head/trace）
 *      缩进恰好 4 个空格；本仓库不用 YAML 锚点、合并键或 flow 风格表达路径；
 *   3. core 路径常量文件（packages/core/src/api/API_PATHS.ts，其次 endpoints.ts）中，
 *      路径以单/双引号字符串字面量出现且以 `/` 开头；不支持含 `${}` 的模板字符串；
 *      `:param` 形式的动态段会被归一化为 `{param}` 再比对；
 *   4. `/healthz` 是运维探针而非客户端 API，不参与对齐（core 不必收录）。
 */

import { readFileSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
const openapiPath = join(repoRoot, 'contracts', 'openapi.yaml');
const coreCandidates = [
  join(repoRoot, 'packages', 'core', 'src', 'api', 'API_PATHS.ts'),
  join(repoRoot, 'packages', 'core', 'src', 'api', 'endpoints.ts'),
];

const HTTP_METHODS = new Set([
  'get',
  'post',
  'put',
  'patch',
  'delete',
  'options',
  'head',
  'trace',
]);

/** 从 openapi.yaml 文本提取 { path -> Set<method> }，不做完整 YAML 解析。 */
function extractOpenApiPaths(source) {
  const lines = source.split(/\r?\n/);
  const paths = new Map();
  let inPaths = false;
  for (const raw of lines) {
    const line = raw.replace(/\t/g, '  ');
    if (!inPaths) {
      if (/^paths:\s*$/.test(line)) inPaths = true;
      continue;
    }
    // paths: 段在下一个顶格键（如 components:）处结束。
    if (/^[A-Za-z0-9_-]+:/.test(line)) break;

    const pathMatch = /^ {2}(?:'([^']+)'|"([^"]+)"|([^\s#:]+)):\s*(?:#.*)?$/.exec(line);
    if (pathMatch) {
      const path = pathMatch[1] ?? pathMatch[2] ?? pathMatch[3];
      if (path.startsWith('/')) {
        paths.set(path, new Set());
        continue;
      }
    }
    const methodMatch = /^ {4}([a-z]+):(?:\s|$)/.exec(line);
    if (methodMatch && HTTP_METHODS.has(methodMatch[1])) {
      const current = [...paths.keys()].pop();
      if (current !== undefined) paths.get(current).add(methodMatch[1]);
    }
  }
  return paths;
}

/**
 * 提取 core 的端点操作表。
 * 首选：`export const API_PATHS = {...}` 中的 `'METHOD /path'` 条目（方法+路径配对，最准确）；
 * 回退：文件中所有以 / 开头的字符串字面量（仅路径级比对）。
 */
function extractCoreOperations(source) {
  const withoutComments = source
    .replace(/\/\*[\s\S]*?\*\//g, ' ')
    .replace(/(^|[^:])\/\/.*$/gm, '$1');
  const paths = new Set();
  const operations = new Set();

  const block = /export\s+const\s+API_PATHS\s*=\s*\{([\s\S]*?)\n\}\s*(?:as\s+const)?/.exec(withoutComments);
  if (block) {
    const entry = /'([A-Z]+)\s+(\/[^']*)'|"([A-Z]+)\s+(\/[^"]*)"/g;
    let match;
    while ((match = entry.exec(block[1])) !== null) {
      const method = (match[1] ?? match[3]).toLowerCase();
      const path = normalizePath(match[2] ?? match[4]);
      paths.add(path);
      operations.add(`${method.toUpperCase()} ${path}`);
    }
  }

  if (operations.size === 0) {
    const literal = /(['"])((?:\\.|(?!\1).)*)\1/g;
    let match;
    while ((match = literal.exec(withoutComments)) !== null) {
      const raw = match[2];
      if (!raw.startsWith('/') || raw.includes('${')) continue;
      if (!/^\/[A-Za-z0-9_\-./{}:]*$/.test(raw)) continue;
      paths.add(normalizePath(raw));
    }
  }

  return { paths, operations, paired: operations.size > 0 };
}

/** `:param` 动态段归一化为 `{param}`，与 openapi 模板对齐。 */
function normalizePath(path) {
  return path.replace(/\/:([A-Za-z0-9_]+)(?=\/|$)/g, '/{$1}');
}

function main() {
  if (!existsSync(openapiPath)) {
    console.error(`[check-sync] 找不到 ${openapiPath}`);
    process.exit(1);
  }
  const openapiPaths = extractOpenApiPaths(readFileSync(openapiPath, 'utf8'));
  if (openapiPaths.size === 0) {
    console.error('[check-sync] 未从 openapi.yaml 解析到任何路径（格式假设失效？见脚本头部注释）');
    process.exit(1);
  }

  const operationCount = [...openapiPaths.values()].reduce(
    (sum, methods) => sum + methods.size,
    0,
  );
  console.log(
    `[check-sync] openapi.yaml: ${openapiPaths.size} 个路径 / ${operationCount} 个操作`,
  );

  const coreFile = coreCandidates.find((candidate) => existsSync(candidate));
  if (!coreFile) {
    console.log('[check-sync] core 未就绪，跳过（未找到 packages/core/src/api/API_PATHS.ts 或 endpoints.ts）');
    process.exit(0);
  }

  const { paths: corePaths, operations: coreOps, paired } = extractCoreOperations(
    readFileSync(coreFile, 'utf8'),
  );
  console.log(
    `[check-sync] ${coreFile}: ${paired ? `${coreOps.size} 个端点操作（方法+路径配对）` : `${corePaths.size} 个路径常量（回退模式）`}`,
  );

  // /healthz 为运维探针，不要求 core 收录。
  const specPaths = new Set(
    [...openapiPaths.keys()].filter((path) => path !== '/healthz'),
  );
  const specOps = new Set();
  for (const [path, methods] of openapiPaths) {
    if (path === '/healthz') continue;
    for (const method of methods) specOps.add(`${method.toUpperCase()} ${path}`);
  }

  let onlyInSpec;
  let onlyInCore;
  if (paired) {
    onlyInSpec = [...specOps].filter((op) => !coreOps.has(op)).sort();
    onlyInCore = [...coreOps].filter((op) => !specOps.has(op)).sort();
  } else {
    onlyInSpec = [...specPaths].filter((path) => !corePaths.has(path)).sort();
    onlyInCore = [...corePaths].filter((path) => !specPaths.has(path)).sort();
  }

  if (onlyInSpec.length === 0 && onlyInCore.length === 0) {
    console.log('[check-sync] ✅ 契约与 core 端点完全一致');
    process.exit(0);
  }

  if (onlyInSpec.length > 0) {
    console.error('[check-sync] 仅存在于 openapi.yaml（core 缺失）:');
    for (const op of onlyInSpec) console.error(`  - ${op}`);
  }
  if (onlyInCore.length > 0) {
    console.error('[check-sync] 仅存在于 core（openapi.yaml 未定义）:');
    for (const op of onlyInCore) console.error(`  - ${op}`);
  }
  console.error('[check-sync] ❌ 契约与 core 端点不一致');
  process.exit(1);
}

main();
