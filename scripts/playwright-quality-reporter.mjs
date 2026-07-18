const WINDOWS_SKIP_FILE = 'windows-headed-pwa.acceptance.spec.ts';
const KEY_FLOWS_FILE = 'key_flows_e2e.spec.ts';
const FALLBACK_TITLE = '进入播放路由即发起编码协商';
const PRIMARY_TITLE = '打开真实媒体的播放页：容器挂载且发起编码协商';
const TITLE_SEPARATOR = ' › ';

export function toQualityRecord(test, result) {
  return {
    file: test.location.file,
    title: test.titlePath().join(TITLE_SEPARATOR),
    status: result.status,
    retry: result.retry,
  };
}

export function collectQualityViolations(records) {
  const violations = [];

  for (const record of records) {
    if (record.retry > 0) {
      violations.push(`用例发生重试：${record.title}`);
    }
    if (record.status === 'skipped' && !isAllowedSkip(record, records)) {
      violations.push(`用例被跳过：${record.title}`);
    }
  }

  return violations;
}

function isAllowedSkip(record, records) {
  const fileName = getFileName(record.file);
  if (fileName === WINDOWS_SKIP_FILE) {
    return true;
  }
  if (fileName !== KEY_FLOWS_FILE || getTestTitle(record.title) !== FALLBACK_TITLE) {
    return false;
  }

  const primaryResults = records.filter(
    (candidate) =>
      candidate.file === record.file && getTestTitle(candidate.title) === PRIMARY_TITLE,
  );
  const finalPrimaryResult = primaryResults.reduce(
    (latest, candidate) => (!latest || candidate.retry >= latest.retry ? candidate : latest),
    undefined,
  );
  return finalPrimaryResult?.status === 'passed';
}

function getFileName(file) {
  return file.replaceAll('\\', '/').split('/').at(-1);
}

function getTestTitle(fullTitle) {
  return fullTitle.split(TITLE_SEPARATOR).at(-1);
}

export default class PlaywrightQualityReporter {
  constructor() {
    this.records = [];
  }

  onTestEnd(test, result) {
    this.records.push(toQualityRecord(test, result));
  }

  onEnd(result) {
    try {
      if (result.status !== 'passed') {
        return undefined;
      }

      const violations = collectQualityViolations(this.records);
      if (violations.length === 0) {
        return undefined;
      }

      console.error(`Playwright 质量门禁失败：\n${violations.join('\n')}`);
      return { status: 'failed' };
    } catch {
      console.error('Playwright 质量报告器执行异常。');
      return { status: 'failed' };
    }
  }
}
