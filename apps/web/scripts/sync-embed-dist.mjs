/**
 * FR2-067：把 apps/web/dist 同步到 apps/server/web/dist，供 go:embed 使用。
 * （不再写入根 frontend/dist；根 frontend shim 已废弃。）
 */
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(__dirname, '..');
const src = path.join(webRoot, 'dist');
const dest = path.resolve(webRoot, '../server/web/dist');

if (!fs.existsSync(src)) {
    console.error(`[sync-embed-dist] 源目录不存在: ${src}`);
    process.exit(1);
}

fs.mkdirSync(path.dirname(dest), { recursive: true });
fs.rmSync(dest, { recursive: true, force: true });
fs.cpSync(src, dest, { recursive: true });
// 确保目录可被 go:embed 识别（至少含文件）
const marker = path.join(dest, '.embed-ok');
if (!fs.existsSync(path.join(dest, 'index.html'))) {
    fs.writeFileSync(marker, 'ok\n');
}
console.log(`[sync-embed-dist] 已同步 ${src} → ${dest}`);
