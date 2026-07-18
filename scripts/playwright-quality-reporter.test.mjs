import assert from 'node:assert/strict';
import test from 'node:test';

import PlaywrightQualityReporter, {
  collectQualityViolations,
  toQualityRecord,
} from './playwright-quality-reporter.mjs';

const keyFlowsFile = 'D:\\Projects\\JianVideo\\e2e\\key_flows_e2e.spec.ts';

function record(file, title, status, retry = 0) {
  return { file, title, status, retry };
}

test('普通通过', () => {
  const qualityRecord = toQualityRecord(
    {
      location: { file: 'e2e/example.spec.ts' },
      titlePath: () => ['example.spec.ts', '播放套件', '正常播放'],
    },
    { status: 'passed', retry: 0 },
  );

  assert.deepEqual(qualityRecord, {
    file: 'e2e/example.spec.ts',
    title: 'example.spec.ts › 播放套件 › 正常播放',
    status: 'passed',
    retry: 0,
  });
  assert.deepEqual(collectQualityViolations([qualityRecord]), []);
});

test('允许 Windows 有头 PWA 用例跳过', () => {
  const results = [
    record('e2e/windows-headed-pwa.acceptance.spec.ts', 'Windows 环境检查', 'skipped'),
  ];

  assert.deepEqual(collectQualityViolations(results), []);
});

test('强路径通过后允许降级路径跳过', () => {
  const results = [
    record(
      keyFlowsFile,
      'key_flows_e2e.spec.ts › 打开真实媒体的播放页：容器挂载且发起编码协商',
      'passed',
    ),
    record(
      keyFlowsFile,
      'key_flows_e2e.spec.ts › 进入播放路由即发起编码协商',
      'skipped',
    ),
  ];

  assert.deepEqual(collectQualityViolations(results), []);
});

test('强路径未通过时阻断降级路径跳过', () => {
  const results = [
    record(
      keyFlowsFile,
      'key_flows_e2e.spec.ts › 打开真实媒体的播放页：容器挂载且发起编码协商',
      'failed',
    ),
    record(
      keyFlowsFile,
      'key_flows_e2e.spec.ts › 进入播放路由即发起编码协商',
      'skipped',
    ),
  ];

  assert.equal(collectQualityViolations(results).length, 1);
});

test('阻断其他跳过', () => {
  const results = [record('e2e/other.spec.ts', '其他用例', 'skipped')];

  assert.equal(collectQualityViolations(results).length, 1);
});

test('阻断任何重试', () => {
  const results = [record('e2e/example.spec.ts', '重试后通过', 'passed', 1)];

  assert.equal(collectQualityViolations(results).length, 1);
});

test('原始结果失败时不覆盖为成功', () => {
  const reporter = new PlaywrightQualityReporter();

  assert.equal(reporter.onEnd({ status: 'failed' }), undefined);
});

test('结束处理异常时输出中文错误并返回失败', () => {
  const reporter = new PlaywrightQualityReporter();
  const messages = [];
  const originalError = console.error;
  console.error = (message) => messages.push(message);

  try {
    assert.deepEqual(reporter.onEnd(null), { status: 'failed' });
  } finally {
    console.error = originalError;
  }

  assert.match(messages.join('\n'), /执行异常/);
});
