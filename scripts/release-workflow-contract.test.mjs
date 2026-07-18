// 发布工作流静态契约：锁定触发面、门禁顺序、固定 SHA 与最小写权限。
import assert from 'node:assert/strict'
import { existsSync, readFileSync } from 'node:fs'
import { test } from 'node:test'

const read = (path) => readFileSync(new URL(`../${path}`, import.meta.url), 'utf8')
const workflows = {
  build: read('.github/workflows/build.yml'),
  ci: read('.github/workflows/ci.yml'),
  experimental: read('.github/workflows/experimental.yml'),
  rc: read('.github/workflows/rc.yml'),
  release: read('.github/workflows/release.yml'),
}
const publishScript = read('scripts/publish-release.sh')

const expectedActionPins = new Map([
  ['actions/checkout', '11bd71901bbe5b1630ceea73d27597364c9af683'],
  ['actions/setup-go', '40f1582b2485089dde7abd97c1529aa768e1baff'],
  ['actions/setup-node', '49933ea5288caeca8642d1e84afbd3f7d6820020'],
  ['actions/upload-artifact', 'ea165f8d65b6e75b540449e92b4886f43607fa02'],
  ['actions/download-artifact', 'd3f86a106a0bac45b974a628896c90dbdf5c8093'],
])

const assertPinnedActions = (content) => {
  for (const match of content.matchAll(/uses:\s+([^\s]+)@([^\s]+)/g)) {
    if (match[1].startsWith('./')) continue
    assert.match(match[2], /^[0-9a-f]{40}$/, `Action 必须固定完整提交：${match[0]}`)
    assert.equal(match[2], expectedActionPins.get(match[1]), `Action 提交必须属于正确仓库：${match[0]}`)
  }
}

const runCommands = (content) => {
  const lines = content.split('\n')
  const commands = []
  for (let index = 0; index < lines.length; index += 1) {
    const match = lines[index].match(/^(\s*)run:\s*(.*)$/)
    if (!match) continue
    const indentation = match[1].length
    const command = [match[2]]
    while (index + 1 < lines.length) {
      const next = lines[index + 1]
      if (next.trim() && next.match(/^\s*/)[0].length <= indentation) break
      command.push(next)
      index += 1
    }
    commands.push(command.join('\n'))
  }
  return commands
}

test('ci 直接覆盖 main，dev 由实验工作流复用，且工作区门运行发布契约', () => {
  assert.match(workflows.ci, /workflow_call:/)
  assert.match(workflows.ci, /push:[\s\S]*branches: \[main\]/)
  assert.doesNotMatch(workflows.ci, /branches: \[[^\]]*dev/)
  assert.match(workflows.experimental, /uses: \.\/\.github\/workflows\/ci\.yml/)
  for (const job of ['workspace-quality', 'go-quality', 'web-quality', 'e2e']) {
    assert.match(workflows.ci, new RegExp(`^  ${job}:`, 'm'))
  }
  const workspaceJob = workflows.ci.slice(
    workflows.ci.indexOf('  workspace-quality:'),
    workflows.ci.indexOf('\n  go-quality:'),
  )
  assert.match(workspaceJob, /pnpm quality:release/)
})

test('实验构建只由 dev push 触发且没有发布权限', () => {
  assert.match(workflows.experimental, /push:[\s\S]*branches: \[dev\]/)
  assert.doesNotMatch(workflows.experimental, /workflow_dispatch:/)
  assert.doesNotMatch(workflows.experimental, /contents: write/)
  assert.doesNotMatch(workflows.experimental, /publish-release\.sh/)
  assert.match(workflows.experimental, /needs: \[prepare, quality\]/)
})

test('RC 与 GA 仅手动运行，安全读取分支名并显式拒绝非 main', () => {
  for (const content of [workflows.rc, workflows.release]) {
    assert.match(content, /workflow_dispatch:/)
    assert.doesNotMatch(content, /^  push:/m)
    assert.match(content, /env:\n\s+REF_NAME: \$\{\{ github\.ref_name \}\}/)
    assert.match(content, /run: test "\$REF_NAME" = "main"/)
    for (const command of runCommands(content)) {
      assert.doesNotMatch(command, /\$\{\{\s*github\.ref_name\s*\}\}/)
    }
    assert.match(content, /uses: \.\/\.github\/workflows\/ci\.yml/)
    assert.match(content, /uses: \.\/\.github\/workflows\/build\.yml/)
    assert.match(content, /needs: \[prepare, quality, build\]/)
  }
})

test('RC 与 GA 发布说明严格取自自身版本段且禁止兜底', () => {
  assert.match(workflows.rc, /changelog-extract\.sh "\$\{\{ needs\.prepare\.outputs\.version \}\}" CHANGELOG\.md > release-notes\.md/)
  assert.doesNotMatch(workflows.rc, /changelog-extract\.sh unreleased/)
  assert.doesNotMatch(workflows.rc, /暂无|fallback/i)
  assert.match(workflows.release, /changelog-extract\.sh "\$\{\{ needs\.prepare\.outputs\.version \}\}" CHANGELOG\.md > release-notes\.md/)
  assert.doesNotMatch(workflows.release, /暂无|fallback/i)
})

test('RC 固定预发布，GA 固定稳定发布并绑定生产环境', () => {
  assert.match(workflows.rc, /publish-release\.sh publish[\s\S]* rc release-notes\.md/)
  assert.doesNotMatch(workflows.rc, /environment: production/)
  assert.match(workflows.release, /environment: production/)
  assert.match(workflows.release, /publish-release\.sh publish[\s\S]* ga release-notes\.md/)
  assert.match(publishScript, /rc\)[\s\S]*make_latest=false/)
  assert.match(publishScript, /ga\)[\s\S]*make_latest=true/)
})

test('最终 RC 后的允许列表保持为正式发布文档收口', () => {
  assert.match(
    publishScript,
    /VERSION\|CHANGELOG\.md\|README\.md\|docs\/\*\|\.claude\/rules\/scope-discipline\.md/,
  )
})

test('构建固定调用方 source SHA', () => {
  assert.match(workflows.build, /ref: \$\{\{ inputs\.source_sha \}\}/)
  assert.match(workflows.build, /git rev-parse HEAD/)
  for (const content of [workflows.experimental, workflows.rc, workflows.release]) {
    assert.match(content, /source_sha:/)
  }
})

test('构建保留天数由各发布通道显式传入', () => {
  assert.match(workflows.build, /artifact_retention_days:[\s\S]*required: true[\s\S]*type: number/)
  assert.match(workflows.build, /retention-days: \$\{\{ inputs\.artifact_retention_days \}\}/)
  assert.match(workflows.experimental, /artifact_retention_days: 7/)
  assert.match(workflows.rc, /artifact_retention_days: 14/)
  assert.match(workflows.release, /artifact_retention_days: 7/)
})

test('RC 与 GA 在质量和构建前完成目标引用预检', () => {
  const rcPrepareEnd = workflows.rc.indexOf('\n  quality:')
  const rcPreflight = workflows.rc.indexOf('publish-release.sh preflight-rc')
  assert.ok(rcPreflight > 0 && rcPreflight < rcPrepareEnd)

  const gaPrepareEnd = workflows.release.indexOf('\n  quality:')
  const gaPreflight = workflows.release.indexOf('publish-release.sh preflight ')
  assert.ok(gaPreflight > 0 && gaPreflight < gaPrepareEnd)

  for (const content of [workflows.rc, workflows.release]) {
    assert.equal([...content.matchAll(/fetch-depth: 0/g)].length, 2)
  }
})

test('GA 在 prepare 阶段预检最终 RC，并在打标签前复检', () => {
  const qualityStart = workflows.release.indexOf('\n  quality:')
  const publishJob = workflows.release.indexOf('\n  publish:')
  const publishCommand = workflows.release.indexOf('publish-release.sh publish', publishJob)
  const verifies = [...workflows.release.matchAll(/publish-release\.sh verify-final-rc/g)]
  assert.equal(verifies.length, 2)
  assert.ok(verifies[0].index > 0 && verifies[0].index < qualityStart)
  assert.ok(verifies[1].index > publishJob && verifies[1].index < publishCommand)
})

test('RC 与 GA 共用公开发布串行锁', () => {
  for (const content of [workflows.rc, workflows.release]) {
    assert.match(content, /concurrency:[\s\S]*group: release-publication[\s\S]*cancel-in-progress: false/)
  }
})

test('只有发布 job 获得 contents 写权限', () => {
  assert.doesNotMatch(workflows.build, /contents: write/)
  assert.doesNotMatch(workflows.ci, /contents: write/)
  assert.doesNotMatch(workflows.experimental, /contents: write/)
  for (const content of [workflows.rc, workflows.release]) {
    const occurrences = [...content.matchAll(/contents: write/g)]
    assert.equal(occurrences.length, 1)
  }
})

test('所有外部 Action 固定完整提交', () => {
  Object.values(workflows).forEach(assertPinnedActions)
})

test('旧预发布工作流已被 experimental 替换', () => {
  const oldPath = new URL('../.github/workflows/prerelease.yml', import.meta.url)
  assert.equal(existsSync(oldPath), false)
})

test('根脚本提供统一发布质量入口并纳入总质量门', () => {
  const packageJson = JSON.parse(read('package.json'))
  assert.equal(
    packageJson.scripts['quality:release'],
    'bash scripts/dev-version_test.sh && bash scripts/changelog-extract_test.sh && bash scripts/publish-release_test.sh && node --test scripts/release-workflow-contract.test.mjs',
  )
  assert.match(packageJson.scripts.quality, /pnpm run quality:release/)
})
