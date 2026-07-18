import { describe, expect, it } from "vitest";
import { loadClientDemoSnapshot } from "./clientDemo";

describe("wiki client demo", () => {
  it("展示 API client 列表、详情、分页、任务轮询和 Space 切换", async () => {
    const snapshot = await loadClientDemoSnapshot();

    expect(snapshot.defaultSpace.firstPageTitles).toEqual(["家庭素材 001"]);
    expect(snapshot.defaultSpace.secondPageTitles).toEqual(["家庭素材 002"]);
    expect(snapshot.detailTitle).toBe("家庭素材 001");
    expect(snapshot.taskStatuses).toEqual(["running", "succeeded"]);
    expect(snapshot.taskPollInterval).toBe(2_000);
    expect(snapshot.studioSpace.firstPageTitles).toEqual(["工作室素材 001"]);
  });
});
