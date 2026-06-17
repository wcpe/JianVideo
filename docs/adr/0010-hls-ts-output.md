# ADR-0010：HLS 切片写入与会话管理架构

## 状态
已接受

## 背景
FR-06 需要实现 HLS（m3u8 + TS 切片）的强制输出能力。需要决定切片写入和会话管理的设计方式：如何在内存中管理切片状态、如何组织 m3u8 的追播模式、如何暴露 HTTP 接口。

## 决策
采用两级架构：`HLSSegmentWriter`（单会话写入器）+ `HLSManager`（全局会话管理器）。

- `HLSSegmentWriter` 负责管理单个媒体文件的切片写入和 m3u8 索引更新。每次写入一个 TS 切片后，在 m3u8 文件末尾追加一条 `#EXTINF` 记录，不写 `EXT-X-ENDLIST`（追播模式）。
- `HLSManager` 负责管理所有活跃的 `HLSSegmentWriter` 实例，以 `media_id` 为 key 进行 Get/Create/Remove 操作。
- 切片目录结构：`data/hls/{media_id}/index.m3u8`，`data/hls/{media_id}/segment_NNN.ts`。
- API 层直接调用 HLSManager 读取 m3u8 和切片文件，返回给前端。

## 理由
- **职责分离**：Writer 只管写入和索引更新，Manager 只管会话生命周期，API 只管 HTTP 适配。
- **追播友好**：每次写入后追加 m3u8 记录，前端轮询 m3u8 即可发现新切片，无需等待 `EXT-X-ENDLIST`。
- **无外部依赖**：纯文件操作，不引入消息队列或共享内存。
- **测试友好**：Writer 和 Manager 均可通过接口 mock 进行单元测试。

## 后果
- 切片文件在转码结束后保留在磁盘上，暂不清理（后续版本处理）。
- 内存中维护 Writer 实例映射，进程重启后丢失状态（可接受，转码会话也会丢失）。

## 备选方案
- **FFmpeg hls muxer**：FFmpeg 自带的 `hls` muxer 可直接生成切片和 m3u8，但灵活性不足（追播模式控制困难），且需要完整 CGO 管道就绪。当前方案先自行管理切片，后续可切换。
- **数据库存储切片元数据**：增加复杂度，单用户场景文件系统足够。
