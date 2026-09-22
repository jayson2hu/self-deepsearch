import assert from "node:assert/strict";
import test from "node:test";
import { collectorCandidateHref, collectorListHref, readCollectorSearch } from "./navigation.ts";

const id = "12345678-1234-1234-1234-123456789abc";

test("candidate and return links preserve only the current review context", () => {
  const search = readCollectorSearch({ q: "人物 & 别名", status: "pending", batch_id: id, offset: "50", batch_offset: "10", returnTo: "https://outside.test/" });
  const detail = new URL(collectorCandidateHref(id, search), "https://ops.test");
  assert.equal(detail.pathname, `/collector/${id}`);
  assert.equal(detail.searchParams.get("q"), "人物 & 别名");
  assert.equal(detail.searchParams.has("returnTo"), false);
  const returned = new URL(collectorListHref(readCollectorSearch(Object.fromEntries(detail.searchParams))), "https://ops.test");
  assert.equal(returned.pathname, "/collector");
  assert.deepEqual(Object.fromEntries(returned.searchParams), search);
  assert.equal(returned.hash, "#candidates");
});

test("pagination and batch changes preserve independent filters and reset candidate offset explicitly", () => {
  const search = { q: "name", status: "pending", offset: "50", batch_offset: "10" };
  const href = new URL(collectorListHref(search, { batch_id: id, offset: undefined }), "https://ops.test");
  assert.equal(href.searchParams.get("q"), "name");
  assert.equal(href.searchParams.get("batch_offset"), "10");
  assert.equal(href.searchParams.has("offset"), false);
  assert.equal(new URL(collectorListHref(search, { batch_offset: "20" }, "batches"), "https://ops.test").hash, "#batches");
});

test("malformed or ambiguous query values cannot produce external navigation or invalid API filters", () => {
  assert.deepEqual(readCollectorSearch({ q: ["one", "two"], status: "published", batch_id: "../bad", offset: "-1", batch_offset: "1000001" }), {});
  assert.deepEqual(readCollectorSearch({ q: "x\0y", offset: "NaN", batch_offset: "1.1" }), {});
  assert.equal(collectorCandidateHref("../../outside", {}), "/collector#candidates");
  const href = new URL(collectorListHref({ q: "//outside.test/<script>&offset=999" }), "https://ops.test");
  assert.equal(href.origin, "https://ops.test");
  assert.equal(href.pathname, "/collector");
  assert.equal(href.searchParams.get("offset"), null);
  assert.deepEqual(readCollectorSearch({ q: " 名字 ", offset: "00025" }), { q: "名字", offset: "25" });
});
