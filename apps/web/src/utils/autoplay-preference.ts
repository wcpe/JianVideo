/** 剧集/合集自动连播偏好（FR2-047），持久化到 localStorage。 */

const STORAGE_KEY = 'jianvideo-autoplay-next';

/** 读取是否开启自动连播；缺省为 true。 */
export function readAutoplayNext(): boolean {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw === null) return true;
    return raw !== '0' && raw !== 'false';
  } catch {
    return true;
  }
}

/** 写入自动连播偏好。 */
export function writeAutoplayNext(enabled: boolean): void {
  try {
    localStorage.setItem(STORAGE_KEY, enabled ? '1' : '0');
  } catch {
    /* 隐私模式等写失败时忽略 */
  }
}
