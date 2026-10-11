import { describe, expect, it } from "vitest";
import { bandwidthLimit } from "./gatewayLimit";

describe("bandwidthLimit", () => {
  it("says a number of Mbps for a capped gateway", () => {
    expect(bandwidthLimit(50)).toEqual({ text: "50 Mbps", reported: true });
  });
  it("says there is no limit only when the gateway reported 0", () => {
    expect(bandwidthLimit(0)).toEqual({ text: "No limit", reported: true });
  });
  it("never turns a gateway that did not report into 'no limit'", () => {
    expect(bandwidthLimit(null)).toEqual({ text: "Not reported", reported: false });
    expect(bandwidthLimit(undefined)).toEqual({ text: "Not reported", reported: false });
  });
});
