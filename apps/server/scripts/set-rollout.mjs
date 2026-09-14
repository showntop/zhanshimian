#!/usr/bin/env node
// set-rollout.mjs — 放量阶梯编辑器:把 ai-routing.production.json 的
// release.candidate_percent 改为 0/5/25/50/100 之一,其他值退出 2。
// 回滚即 `--percent 0`;禁止手写绕过阶梯(与 provider/ai 白名单一致)。
import { readFile, writeFile, rename } from 'node:fs/promises';

const ALLOWED = new Set([0, 5, 25, 50, 100]);
const CONFIG_URL = new URL('../config/ai-routing.production.json', import.meta.url);

function parsePercent(argv) {
  const index = argv.indexOf('--percent');
  if (index === -1 || index + 1 >= argv.length) {
    return null;
  }
  const raw = argv[index + 1];
  if (!/^-?\d+$/.test(raw)) {
    return null;
  }
  return Number.parseInt(raw, 10);
}

const percent = parsePercent(process.argv.slice(2));
if (percent === null || !ALLOWED.has(percent)) {
  console.error('usage: node apps/server/scripts/set-rollout.mjs --percent 0|5|25|50|100');
  process.exit(2);
}

const config = JSON.parse(await readFile(CONFIG_URL, 'utf8'));
if (!config.release || typeof config.release !== 'object') {
  console.error('ai-routing.production.json is missing the release block');
  process.exit(2);
}
config.release.candidate_percent = percent;

// 原子写入:先写临时文件再 rename,不能让截断的半份配置进入部署。
const tmpUrl = new URL('../config/ai-routing.production.json.tmp', import.meta.url);
await writeFile(tmpUrl, JSON.stringify(config, null, 2) + '\n');
await rename(tmpUrl, CONFIG_URL);

console.log(`candidate_percent=${percent}`);
