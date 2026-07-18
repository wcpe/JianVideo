import { describe, expect, it } from "vitest";
import {
  filterWikiCatalog,
  getWikiScenarioSummary,
  listWikiGroups,
  listWikiScenarioTitles,
  scanWikiMockSensitiveInfo,
  wikiPreviewCatalog,
} from "./catalog";

describe("wiki catalog", () => {
  it("组件代码片段只指向 packages 导入路径", () => {
    expect(
      wikiPreviewCatalog.every((item) =>
        item.snippet.importPath.startsWith("@jianvideo/"),
      ),
    ).toBe(true);
  });

  it("支持搜索、分组与 HLS 场景路径", () => {
    const results = filterWikiCatalog({ group: "media", query: "HLS" });

    expect(results.map((item) => item.id)).toEqual(["hls-preview-card"]);
    expect(results[0]?.snippet.code).toContain("from '@jianvideo/ui'");
  });

  it("列出 wiki 预期分组", () => {
    expect(listWikiGroups().map((group) => group.id)).toEqual([
      "basic",
      "media",
      "task",
      "space",
      "pixi",
      "theme",
    ]);
  });

  it("展示共享 mock 场景", () => {
    expect(listWikiScenarioTitles()).toContain("百万素材压力场景");
  });

  it("登记 API client 示例", () => {
    expect(wikiPreviewCatalog.map((item) => item.id)).toContain(
      "api-client-demo",
    );
  });

  it("场景切换能返回对应 mock 摘要", () => {
    expect(getWikiScenarioSummary("hls-pending")).toContain("HLS");
    expect(getWikiScenarioSummary("permission-denied")).toContain("Space");
  });

  it("wiki mock 数据不包含敏感信息", () => {
    expect(scanWikiMockSensitiveInfo()).toEqual([]);
  });
});
