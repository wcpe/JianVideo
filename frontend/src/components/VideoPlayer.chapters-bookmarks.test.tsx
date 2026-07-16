import { PlaybackCore } from '@jianvideo/player-core';
import type { MediaBookmark, MediaChapter } from '../../../packages/media-client/src/index';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MantineProvider } from '@mantine/core';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import VideoPlayer from './VideoPlayer';

vi.mock('mpegts.js', () => ({ default: { createPlayer: () => ({}) } }));
vi.mock('hls.js', () => ({
  default: class {
    static isSupported() {
      return false;
    }
  },
}));

const chapters: readonly MediaChapter[] = [
  {
    endMs: 30_000,
    id: 'chapter-opening',
    language: 'zh',
    source: 'embedded',
    sourceIndex: 0,
    startMs: 0,
    title: '开场',
  },
  {
    endMs: 80_000,
    id: 'chapter-main',
    language: 'zh',
    source: 'embedded',
    sourceIndex: 1,
    startMs: 30_000,
    title: '主体',
  },
];

const bookmark: MediaBookmark = {
  createdAt: '2026-07-13T00:00:00Z',
  id: 'bookmark-1',
  note: '稍后复看',
  positionMs: 5_000,
  revision: 3,
  title: '重点',
  updatedAt: '2026-07-13T00:01:00Z',
};

function stubMedia() {
  const prototype = Object.getPrototypeOf(
    Object.getPrototypeOf(document.createElement('video')),
  ) as HTMLMediaElement;
  Object.defineProperty(prototype, 'play', {
    configurable: true,
    value: vi.fn(() => Promise.resolve()),
    writable: true,
  });
  Object.defineProperty(prototype, 'pause', {
    configurable: true,
    value: vi.fn(),
    writable: true,
  });
}

function stubTimeline(video: HTMLVideoElement, duration = 100) {
  let currentTime = 0;
  Object.defineProperties(video, {
    currentTime: {
      configurable: true,
      get: () => currentTime,
      set: (value: number) => {
        currentTime = value;
      },
    },
    duration: { configurable: true, get: () => duration },
    seekable: {
      configurable: true,
      get: () => ({ end: () => duration, length: 1, start: () => 0 }),
    },
  });
}

function renderPlayer(props: Partial<React.ComponentProps<typeof VideoPlayer>> = {}) {
  const view = render(
    <MantineProvider>
      <VideoPlayer
        autoPlay={false}
        bookmarks={[bookmark]}
        chapters={chapters}
        markerContextKey="space-a:1"
        mediaId={1}
        streamType="mp4"
        url="/chapter.mp4"
        {...props}
      />
    </MantineProvider>,
  );
  const video = view.container.querySelector('video')!;
  stubTimeline(video);
  return { ...view, video };
}

function setPlaybackTime(video: HTMLVideoElement, currentTime: number) {
  act(() => {
    video.currentTime = currentTime;
    video.dispatchEvent(new Event('durationchange'));
    video.dispatchEvent(new Event('timeupdate'));
  });
}

beforeEach(() => {
  stubMedia();
  localStorage.clear();
});

describe('VideoPlayer 章节与书签共享 UI（FR2-060）', () => {
  it('显示可访问标记和当前章节，章节跳转统一进入 PlaybackCore.seek', async () => {
    const seek = vi.spyOn(PlaybackCore.prototype, 'seek');
    const { video } = renderPlayer();
    await waitFor(() => expect(video.getAttribute('src')).toBe('/chapter.mp4'));
    setPlaybackTime(video, 35);

    const chapterMarkers = screen.getAllByTestId('video-chapter-marker');
    const bookmarkMarkers = screen.getAllByTestId('video-bookmark-marker');
    expect(chapterMarkers).toHaveLength(2);
    expect(bookmarkMarkers).toHaveLength(1);
    expect(chapterMarkers[1]).toHaveAttribute('aria-label', '章节 主体，0:30');
    expect(bookmarkMarkers[0]).toHaveAttribute('aria-label', '书签 重点，0:05');
    expect(chapterMarkers.every((marker) => marker.style.pointerEvents === 'none')).toBe(true);
    expect(bookmarkMarkers.every((marker) => marker.style.pointerEvents === 'none')).toBe(true);

    await userEvent.click(screen.getByRole('button', { name: '章节与书签，当前章节 主体' }));
    expect(screen.getByText('当前章节：主体')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '跳转到章节 主体，0:30' })).toHaveAttribute(
      'aria-current',
      'true',
    );

    await userEvent.click(screen.getByRole('button', { name: '跳转到章节 主体，0:30' }));
    expect(seek).toHaveBeenCalledWith(30, 'user');

    const progress = screen.getByTestId('video-progress-preview');
    vi.spyOn(progress, 'getBoundingClientRect').mockReturnValue({
      bottom: 16,
      height: 16,
      left: 0,
      right: 100,
      toJSON: () => ({}),
      top: 0,
      width: 100,
      x: 0,
      y: 0,
    });
    fireEvent.mouseMove(progress, { clientX: 50 });
    expect(screen.getByTestId('timeline-preview-overlay')).toHaveTextContent('0:50');
  });

  it('按当前时间创建书签，并支持标题备注编辑、跳转和明确删除', async () => {
    const createBookmark = vi.fn().mockResolvedValue(undefined);
    const updateBookmark = vi.fn().mockResolvedValue(undefined);
    const deleteBookmark = vi.fn().mockResolvedValue(undefined);
    const seek = vi.spyOn(PlaybackCore.prototype, 'seek');
    const { video } = renderPlayer({
      onCreateBookmark: createBookmark,
      onDeleteBookmark: deleteBookmark,
      onUpdateBookmark: updateBookmark,
    });
    await waitFor(() => expect(video.getAttribute('src')).toBe('/chapter.mp4'));
    setPlaybackTime(video, 12.345);

    await userEvent.click(screen.getByRole('button', { name: '章节与书签，当前章节 开场' }));
    await userEvent.click(screen.getByRole('button', { name: '在当前时间新增书签' }));
    await userEvent.type(screen.getByLabelText('书签标题'), '新书签');
    await userEvent.type(screen.getByLabelText('书签备注'), '复核此处');
    await userEvent.click(screen.getByRole('button', { name: '保存书签' }));

    await waitFor(() =>
      expect(createBookmark).toHaveBeenCalledWith({
        note: '复核此处',
        positionMs: 12_345,
        title: '新书签',
      }),
    );

    await userEvent.click(screen.getByRole('button', { name: '跳转到书签 重点，0:05' }));
    expect(seek).toHaveBeenCalledWith(5, 'user');

    await userEvent.click(screen.getByRole('button', { name: '编辑书签 重点' }));
    const title = screen.getByLabelText('书签标题');
    const note = screen.getByLabelText('书签备注');
    await userEvent.clear(title);
    await userEvent.type(title, '修正重点');
    await userEvent.clear(note);
    await userEvent.type(note, '服务端真源');
    await userEvent.click(screen.getByRole('button', { name: '保存修改' }));

    await waitFor(() =>
      expect(updateBookmark).toHaveBeenCalledWith('bookmark-1', {
        note: '服务端真源',
        positionMs: 5_000,
        revision: 3,
        title: '修正重点',
      }),
    );

    await userEvent.click(screen.getByRole('button', { name: '删除书签 重点，0:05' }));
    expect(screen.getByText('确认删除「重点」0:05？')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: '确认删除书签 重点' }));
    await waitFor(() => expect(deleteBookmark).toHaveBeenCalledWith('bookmark-1', 3));
  });

  it('Space 或媒体上下文切换时关闭旧面板并清理草稿数据', async () => {
    const view = renderPlayer({ onCreateBookmark: vi.fn().mockResolvedValue(undefined) });
    await waitFor(() => expect(view.video.getAttribute('src')).toBe('/chapter.mp4'));
    setPlaybackTime(view.video, 35);
    await userEvent.click(screen.getByRole('button', { name: '章节与书签，当前章节 主体' }));
    await userEvent.click(screen.getByRole('button', { name: '在当前时间新增书签' }));
    await userEvent.type(screen.getByLabelText('书签标题'), '不得泄漏');

    view.rerender(
      <MantineProvider>
        <VideoPlayer
          autoPlay={false}
          bookmarks={[]}
          chapters={[{ ...chapters[0]!, id: 'chapter-new', title: '新空间章节' }]}
          markerContextKey="space-b:1"
          mediaId={1}
          streamType="mp4"
          url="/chapter.mp4"
        />
      </MantineProvider>,
    );

    await waitFor(() => expect(screen.queryByLabelText('书签标题')).not.toBeInTheDocument());
    await userEvent.click(screen.getByRole('button', { name: /章节与书签/ }));
    expect(screen.queryByText('不得泄漏')).not.toBeInTheDocument();
    expect(screen.getByText('新空间章节')).toBeInTheDocument();
    expect(screen.getByText('暂无书签')).toBeInTheDocument();
  });
});
