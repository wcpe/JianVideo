// 开源协议清单生成脚本（FR-57）
//
// 构建期运行，产出 frontend/src/data/licenses.json（入库、随 go:embed 内嵌、运行时不联网）。
// 覆盖三部分：
//   1. 项目自身：读根 LICENSE（MIT 全文 + 版权作者）。
//   2. 前端：license-checker 取全部生产依赖（--production）的 name@version / 协议 / 作者 / 协议全文。
//   3. 后端：解析根 go.mod require 块的直接依赖（剔除 // indirect），尽力从本机 Go module
//      cache 读协议名与全文；读不到则给 pkg.go.dev 协议页外链（不联网拉取）。
//
// 用法：node frontend/scripts/gen-licenses.mjs（或 npm run gen:licenses）

import { readFileSync, writeFileSync, existsSync, readdirSync, mkdirSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync } from 'node:child_process'
import { createRequire } from 'node:module'

const require = createRequire(import.meta.url)
const scriptDir = dirname(fileURLToPath(import.meta.url))
// 仓库根目录（frontend/scripts 的上两级）
const repoRoot = join(scriptDir, '..', '..')
const frontendDir = join(scriptDir, '..')
const outputPath = join(frontendDir, 'src', 'data', 'licenses.json')

// 常见协议文件名（大小写不敏感匹配前缀）
const LICENSE_FILE_PREFIXES = ['license', 'licence', 'copying']

// 在目录下查找协议文件并返回全文，找不到返回空串
function readLicenseFromDir(dir) {
  if (!existsSync(dir)) return ''
  let entries
  try {
    entries = readdirSync(dir)
  } catch {
    return ''
  }
  const hit = entries.find((name) => {
    const lower = name.toLowerCase()
    return LICENSE_FILE_PREFIXES.some((p) => lower.startsWith(p))
  })
  if (!hit) return ''
  try {
    return readFileSync(join(dir, hit), 'utf8')
  } catch {
    return ''
  }
}

// 读项目自身 LICENSE（MIT），并从「Copyright (c) ...」行提取作者
function buildProjectLicense() {
  const text = readFileSync(join(repoRoot, 'LICENSE'), 'utf8')
  const firstLine = text.split('\n')[0].trim()
  // 协议标识取首行的前缀单词（如 "MIT License" → MIT）
  const license = firstLine.replace(/\s*License\s*$/i, '').trim() || firstLine
  const copyrightLine = text.split('\n').find((l) => /copyright/i.test(l)) || ''
  const author = copyrightLine.replace(/copyright\s*\(c\)\s*/i, '').trim()
  return { name: 'JianVideo', license, author, text }
}

// 前端：license-checker 取全部生产依赖
function buildFrontendLicenses() {
  const licenseChecker = require('license-checker')
  // 排除项目自身这个 package（license-checker 会把根包也列进来，显示为 UNLICENSED）
  const selfPkg = JSON.parse(readFileSync(join(frontendDir, 'package.json'), 'utf8'))
  const selfKey = `${selfPkg.name}@${selfPkg.version}`
  const packages = new Promise((resolve, reject) => {
    licenseChecker.init(
      { start: frontendDir, production: true, excludePackages: selfKey },
      (err, pkgs) => {
        if (err) reject(err)
        else resolve(pkgs)
      },
    )
  })
  return packages.then((pkgs) =>
    Object.entries(pkgs)
      .map(([key, info]) => {
        // key 形如 name@version，name 可能含 scope（@scope/pkg），从末尾的 @ 切分版本
        const at = key.lastIndexOf('@')
        const name = key.slice(0, at)
        const version = key.slice(at + 1)
        const licenseField = info.licenses
        const license = Array.isArray(licenseField) ? licenseField.join(', ') : (licenseField || '')
        const text = info.licenseFile && existsSync(info.licenseFile)
          ? readFileSync(info.licenseFile, 'utf8')
          : ''
        return { name, version, license, author: info.publisher || '', text }
      })
      .sort((a, b) => a.name.localeCompare(b.name)),
  )
}

// 解析 go.mod require 块的直接依赖（剔除 // indirect），返回 [{name, version}]
function parseGoDirectDeps() {
  const goMod = readFileSync(join(repoRoot, 'go.mod'), 'utf8')
  const lines = goMod.split('\n')
  const deps = []
  let inRequireBlock = false
  for (const raw of lines) {
    const line = raw.trim()
    if (line.startsWith('require (')) {
      inRequireBlock = true
      continue
    }
    if (inRequireBlock && line === ')') {
      inRequireBlock = false
      continue
    }
    // 单行 require（require module version）也支持
    const single = line.startsWith('require ') && !line.includes('(')
    if (!inRequireBlock && !single) continue
    // 跳过间接依赖
    if (line.includes('// indirect')) continue
    const body = single ? line.replace(/^require\s+/, '') : line
    const parts = body.replace(/\/\/.*$/, '').trim().split(/\s+/)
    if (parts.length >= 2 && parts[0] && parts[1].startsWith('v')) {
      deps.push({ name: parts[0], version: parts[1] })
    }
  }
  return deps
}

// 取 Go module cache 根目录（go env GOMODCACHE），失败返回空串
function goModCacheDir() {
  try {
    return execFileSync('go', ['env', 'GOMODCACHE'], { encoding: 'utf8' }).trim()
  } catch {
    return ''
  }
}

// module 路径转 module cache 目录名：大写字母转为 "!小写"（Go 的转义规则）
function escapeModulePath(modPath) {
  return modPath.replace(/[A-Z]/g, (c) => '!' + c.toLowerCase())
}

// 从协议全文粗略推断协议标识（仅识别常见几种），识别不出返回空串
function guessLicenseName(text) {
  const head = text.slice(0, 600).toLowerCase()
  if (head.includes('mit license') || head.includes('permission is hereby granted, free of charge')) return 'MIT'
  if (head.includes('apache license') && head.includes('2.0')) return 'Apache-2.0'
  if (head.includes('bsd 3-clause') || (head.includes('redistribution') && head.includes('neither the name'))) return 'BSD-3-Clause'
  if (head.includes('bsd 2-clause')) return 'BSD-2-Clause'
  if (head.includes('mozilla public license') && head.includes('2.0')) return 'MPL-2.0'
  if (head.includes('gnu general public license')) return 'GPL'
  if (head.includes('isc license') || head.includes('isc')) return 'ISC'
  return ''
}

// 后端：直接依赖 + 尽力读 module cache 协议全文，读不到给 pkg.go.dev 链接
function buildBackendLicenses() {
  const deps = parseGoDirectDeps()
  const cacheDir = goModCacheDir()
  return deps
    .map(({ name, version }) => {
      let text = ''
      if (cacheDir) {
        const moduleDir = join(cacheDir, `${escapeModulePath(name)}@${version}`)
        text = readLicenseFromDir(moduleDir)
      }
      if (text) {
        return { name, version, license: guessLicenseName(text), text }
      }
      // 拿不到全文：给 pkg.go.dev 协议页外链（不联网拉取）
      return { name, version, license: '', url: `https://pkg.go.dev/${name}@${version}?tab=licenses` }
    })
    .sort((a, b) => a.name.localeCompare(b.name))
}

async function main() {
  const project = buildProjectLicense()
  const frontend = await buildFrontendLicenses()
  const backend = buildBackendLicenses()

  const data = { project, frontend, backend }
  const outDir = dirname(outputPath)
  if (!existsSync(outDir)) mkdirSync(outDir, { recursive: true })
  writeFileSync(outputPath, JSON.stringify(data, null, 2) + '\n', 'utf8')

  const backendWithText = backend.filter((b) => b.text).length
  console.log(`已生成 ${outputPath}`)
  console.log(`  项目协议：${project.name} / ${project.license}`)
  console.log(`  前端依赖：${frontend.length} 项`)
  console.log(`  后端依赖：${backend.length} 项（含全文 ${backendWithText} 项，余下给 pkg.go.dev 链接）`)
}

main().catch((err) => {
  console.error('生成开源协议清单失败：', err)
  process.exit(1)
})
