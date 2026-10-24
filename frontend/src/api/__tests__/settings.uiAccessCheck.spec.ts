import { beforeEach, describe, expect, it, vi } from "vitest";

const { post } = vi.hoisted(() => ({ post: vi.fn() }));
vi.mock("@/api/client", () => ({ apiClient: { post } }));

import settingsAPI, { checkUIAccess } from "@/api/admin/settings";

describe("admin UI access diagnostics API", () => {
  beforeEach(() => post.mockReset());

  it("posts without accepting a client-supplied IP or country", async () => {
    const result = { ip: "8.8.8.8", country: "US", provider: "geojs", cached: false, elapsed_ms: 23 };
    post.mockResolvedValue({ data: result });
    expect(settingsAPI.checkUIAccess).toBe(checkUIAccess);
    await expect(checkUIAccess()).resolves.toEqual(result);
    expect(post).toHaveBeenCalledExactlyOnceWith("/admin/settings/ui-access-check");
  });

  it("returns structured diagnostic failure information", async () => {
    const result = { ip: "172.18.0.1", country: "", provider: "", cached: false, elapsed_ms: 0, error_code: "non_public_ip" };
    post.mockResolvedValue({ data: result });
    await expect(checkUIAccess()).resolves.toEqual(result);
  });
});
