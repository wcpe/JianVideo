import { notifications } from '@mantine/notifications';
import { IconCircleCheck, IconAlertTriangle } from '@tabler/icons-react';
import { createElement } from 'react';

/**
 * 通知样式统一约定（FR-98）：成功用绿色 + 勾选图标、失败用红色 + 警示图标，
 * 自动关闭时长分级（成功较短、失败较长便于阅读）。
 * 各页改用本助手发通知，消除散落的 color/autoClose 不一致。
 */

// 成功通知自动关闭时长（毫秒）：成功无需久留
const SUCCESS_AUTO_CLOSE = 2500;
// 失败通知自动关闭时长（毫秒）：失败留久一点便于用户看清原因
const ERROR_AUTO_CLOSE = 4000;

/** 成功通知：绿色 + 勾选图标，可选标题 */
export function notifySuccess(message: string, title?: string) {
  notifications.show({
    title,
    message,
    color: 'green',
    icon: createElement(IconCircleCheck, { size: 18 }),
    autoClose: SUCCESS_AUTO_CLOSE,
  });
}

/** 失败通知：红色 + 警示图标，可选标题 */
export function notifyError(message: string, title?: string) {
  notifications.show({
    title,
    message,
    color: 'red',
    icon: createElement(IconAlertTriangle, { size: 18 }),
    autoClose: ERROR_AUTO_CLOSE,
  });
}
