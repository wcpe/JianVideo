import { MantineProvider } from '@mantine/core';
import type { TrackSelectionState } from '@jianvideo/player-core';
import type { WebPlaybackTrack } from '@/api/subtitle';
import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import VideoTrackControls, { SubtitleOverlay } from './VideoTrackControls';
import {
  DEFAULT_SUBTITLE_PREFERENCES,
  loadSubtitlePreferences,
} from './VideoTrackControls.helpers';

const tracks: WebPlaybackTrack[] = [
  {
    available: true,
    capability: 'seamless',
    id: 'sub-a',
    kind: 'subtitle',
    label: '字幕 A',
    source: 'sidecar',
  },
  {
    available: true,
    capability: 'seamless',
    id: 'sub-b',
    kind: 'subtitle',
    label: '字幕 B',
    source: 'uploaded',
  },
  {
    available: true,
    capability: 'unsupported',
    id: 'audio-a',
    kind: 'audio',
    label: '中文音轨',
    language: 'zh',
    codec: 'aac',
    channels: 6,
    default: true,
    unsupportedReason: '当前 Web 播放后端不支持切换音轨',
  },
  {
    available: true,
    capability: 'reload',
    id: 'audio-b',
    kind: 'audio',
    label: '英文音轨',
    language: 'en',
    codec: 'aac',
  },
];

function selection(kind: 'audio' | 'subtitle', selected: string | null, effective: string | null) {
  return {
    effectiveTrackId: effective,
    kind,
    selectedTrackId: selected,
    sourceEpoch: 1,
    sourceId: 'source-a',
  } satisfies TrackSelectionState;
}

function renderControls(overrides: Partial<React.ComponentProps<typeof VideoTrackControls>> = {}) {
  const props: React.ComponentProps<typeof VideoTrackControls> = {
    tracks,
    selections: {
      audio: selection('audio', 'audio-a', 'audio-a'),
      subtitle: selection('subtitle', 'sub-b', 'sub-a'),
    },
    preferences: DEFAULT_SUBTITLE_PREFERENCES,
    onPreferencesChange: vi.fn(),
    onSelect: vi.fn(),
    onUpload: vi.fn(),
    onDelete: vi.fn(),
    ...overrides,
  };
  return {
    props,
    ...render(
      <MantineProvider>
        <VideoTrackControls {...props} />
      </MantineProvider>,
    ),
  };
}

describe('VideoTrackControls', () => {
  beforeEach(() => localStorage.clear());

  it('只以 effective 勾选并显示 selected 切换中', async () => {
    renderControls();
    await userEvent.click(screen.getByRole('button', { name: '字幕轨道' }));

    expect(screen.getByText('字幕 A').parentElement).toHaveTextContent('字幕 A');
    expect(screen.getByText(/字幕 B · 切换中/)).toBeInTheDocument();
  });

  it('轨道菜单项向读屏暴露当前播放与切换中状态', async () => {
    renderControls();
    await userEvent.click(screen.getByRole('button', { name: '字幕轨道' }));

    expect(screen.getByRole('menuitem', { name: '关闭字幕' })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: /字幕 A.*当前播放/ })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: /字幕 B · 切换中/ })).toBeInTheDocument();
  });

  it('音轨 unsupported 列表可渲染且 effective 为空时无勾选', async () => {
    const { props } = renderControls({
      selections: {
        audio: selection('audio', 'audio-a', null),
        subtitle: selection('subtitle', 'sub-b', 'sub-a'),
      },
    });
    await userEvent.click(screen.getByRole('button', { name: '音轨' }));

    const item = screen.getByText(
      /中文音轨.*zh.*aac.*6 声道.*默认.*当前 Web 播放后端不支持切换音轨/,
    );
    const button = item.closest('button');
    expect(button).toBeDisabled();
    expect(button?.querySelector('svg')).toBeNull();
    expect(button).not.toHaveTextContent('当前播放');
    await userEvent.click(item);
    expect(props.onSelect).not.toHaveBeenCalledWith('audio', 'audio-a');
  });

  it('单个不可用 unsupported 音轨仍显示信息与不可切换原因', async () => {
    const onSelect = vi.fn();
    const onlyTrack: WebPlaybackTrack = {
      available: true,
      capability: 'unsupported',
      id: 'audio-only',
      kind: 'audio',
      label: '中文音轨',
      language: 'zh',
      codec: 'aac',
      channels: 6,
      default: true,
      unsupportedReason: 'AUDIO_SWITCH_UNSUPPORTED',
    };
    renderControls({
      tracks: [onlyTrack],
      selections: {
        audio: selection('audio', null, null),
        subtitle: selection('subtitle', null, null),
      },
      onSelect,
    });

    await userEvent.click(screen.getByRole('button', { name: '音轨' }));
    const item = screen.getByRole('menuitem', {
      name: /中文音轨.*zh.*aac.*6 声道.*默认.*AUDIO_SWITCH_UNSUPPORTED/,
    });
    expect(item).toBeDisabled();
    await userEvent.click(item);
    expect(onSelect).not.toHaveBeenCalled();
  });

  it('reload 音轨可点击，当前播放只看 effective，目标 selected 显示切换中', async () => {
    const { props } = renderControls({
      selections: {
        audio: selection('audio', 'audio-b', 'audio-a'),
        subtitle: selection('subtitle', null, null),
      },
    });
    await userEvent.click(screen.getByRole('button', { name: '音轨' }));

    expect(screen.getByRole('menuitem', { name: /中文音轨.*当前播放/ })).toBeInTheDocument();
    const target = screen.getByRole('menuitem', { name: /英文音轨.*切换中/ });
    expect(target).not.toBeDisabled();
    await userEvent.click(target);
    expect(props.onSelect).toHaveBeenCalledWith('audio', 'audio-b');
  });

  it('只有一个可切换音轨时隐藏音轨菜单', () => {
    renderControls({ tracks: tracks.filter((track) => track.id !== 'audio-a') });
    expect(screen.queryByRole('button', { name: '音轨' })).not.toBeInTheDocument();
  });

  it('上传 input 接受四种格式且仅 uploaded 提供删除', async () => {
    const { props } = renderControls();
    await userEvent.click(screen.getByRole('button', { name: '字幕轨道' }));
    expect(screen.getByText('删除 字幕 B')).toBeInTheDocument();
    expect(screen.queryByText('删除 字幕 A')).not.toBeInTheDocument();

    const input = screen.getByLabelText('上传字幕文件');
    expect(input).toHaveAttribute('accept', '.srt,.ass,.ssa,.vtt');
    const file = new File(['WEBVTT'], 'sample.vtt', { type: 'text/vtt' });
    fireEvent.change(input, { target: { files: [file] } });
    expect(props.onUpload).toHaveBeenCalledWith(file);
  });

  it('严格校验 localStorage 字幕偏好', () => {
    localStorage.setItem('jianvideo.subtitle.preferences', JSON.stringify({ fontSize: 'giant' }));
    expect(loadSubtitlePreferences()).toEqual(DEFAULT_SUBTITLE_PREFERENCES);
  });

  it('localStorage 写入失败时样式仍立即生效', async () => {
    const setItem = vi.spyOn(localStorage, 'setItem').mockImplementation(() => {
      throw new DOMException('存储空间不足', 'QuotaExceededError');
    });
    const onPreferencesChange = vi.fn();
    renderControls({ onPreferencesChange });
    await userEvent.click(screen.getByRole('button', { name: '字幕轨道' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '字号 大' }));

    expect(onPreferencesChange).toHaveBeenCalledWith({
      ...DEFAULT_SUBTITLE_PREFERENCES,
      fontSize: 'large',
    });
    setItem.mockRestore();
  });

  it('overlay 按纯文本节点渲染恶意内容并应用样式', () => {
    const malicious = '<img src=x onerror=alert(1)>\n<script>alert(1)</script>';
    const { container } = render(
      <MantineProvider>
        <SubtitleOverlay
          currentTime={1.5}
          cues={[{ start: 1, end: 2, text: malicious }]}
          preferences={{
            fontSize: 'large',
            color: '#ffff00',
            backgroundOpacity: 0.7,
            verticalPosition: 16,
          }}
        />
      </MantineProvider>,
    );

    expect(screen.getByTestId('subtitle-overlay').textContent).toBe(malicious);
    expect(container.querySelector('img')).toBeNull();
    expect(container.querySelector('script')).toBeNull();
    expect(screen.getByTestId('subtitle-overlay')).toHaveStyle({ bottom: '16%' });
  });
});
