/** 纹理池 LRU（抽离自 index，供 media-grid-session 使用）。 */

export interface TextureEntry {
  readonly key: string;
  readonly width: number;
  readonly height: number;
  readonly destroy: () => void;
}

export interface TexturePoolOptions {
  readonly maxTextures: number;
  readonly maxBytes: number;
}

export interface TexturePoolStats {
  readonly keys: readonly string[];
  readonly textureCount: number;
  readonly textureMemoryBytes: number;
}

export function estimateTextureMemoryBytes(
  textureCount: number,
  width: number,
  height: number,
): number {
  return textureCount * width * height * 4;
}

function textureEntryBytes(entry: TextureEntry): number {
  return estimateTextureMemoryBytes(1, entry.width, entry.height);
}

export class TexturePool {
  readonly #entries = new Map<string, TextureEntry>();
  readonly #options: TexturePoolOptions;
  #textureMemoryBytes = 0;

  constructor(options: TexturePoolOptions) {
    this.#options = options;
  }

  get(key: string): TextureEntry | undefined {
    const entry = this.#entries.get(key);
    if (entry === undefined) {
      return undefined;
    }
    this.#entries.delete(key);
    this.#entries.set(key, entry);
    return entry;
  }

  put(entry: TextureEntry): void {
    const existing = this.#entries.get(entry.key);
    if (existing !== undefined) {
      this.#textureMemoryBytes -= textureEntryBytes(existing);
      existing.destroy();
      this.#entries.delete(entry.key);
    }
    this.#entries.set(entry.key, entry);
    this.#textureMemoryBytes += textureEntryBytes(entry);
    this.#evictUntilWithinBudget();
  }

  stats(): TexturePoolStats {
    return {
      keys: [...this.#entries.keys()],
      textureCount: this.#entries.size,
      textureMemoryBytes: this.#textureMemoryBytes,
    };
  }

  #evictUntilWithinBudget(): void {
    while (
      this.#entries.size > this.#options.maxTextures ||
      this.#textureMemoryBytes > this.#options.maxBytes
    ) {
      const oldest = this.#entries.entries().next().value;
      if (oldest === undefined) {
        return;
      }
      const [key, entry] = oldest;
      this.#entries.delete(key);
      this.#textureMemoryBytes -= textureEntryBytes(entry);
      entry.destroy();
    }
  }
}

export interface PreviewState {
  readonly hovered: boolean;
  readonly selected: boolean;
}

export function shouldRequestHlsPreview(state: PreviewState): boolean {
  return state.hovered || state.selected;
}

/** 默认纹理池：约 256 张上限、24MB。 */
export function createDefaultMediaTexturePool(): TexturePool {
  return new TexturePool({
    maxTextures: 256,
    maxBytes: 24 * 1024 * 1024,
  });
}
