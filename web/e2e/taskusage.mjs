// Browser check for the "Resource use" card on a customer's task page and the machine page's Network
// tab (follow-up to task 8.15). Needs a task that went through a gateway tunnel on an agent that reports usage.
//
//   BASE=http://127.0.0.1:18081 TASK=tsk_... SHOTS=/tmp/shots node e2e/taskusage.mjs
import puppeteer from "puppeteer-core";
import { chartInfo, hoverFind } from "./chart.mjs";

const BASE = process.env.BASE || "http://127.0.0.1:18081";
const SHOTS = process.env.SHOTS;
const results = [];
const check = (name, ok, detail = "") => {
  results.push({ ok });
  console.log(`${ok ? "PASS" : "FAIL"}  ${name}${detail ? `  (${detail})` : ""}`);
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const browser = await puppeteer.launch({
  executablePath: process.env.CHROME || "/usr/bin/google-chrome",
  headless: "new",
  args: ["--no-sandbox", "--disable-gpu"],
});
let page = await browser.newPage();
await page.setViewport({ width: 1440, height: 1100 });
const errors = [];
page.on("pageerror", (e) => errors.push(String(e)));
const go = async (url) => {
  await page.goto(url, { waitUntil: "domcontentloaded" });
  await page.waitForSelector("main#main", { timeout: 15000 });
  await sleep(1500);
};
const shot = async (name) => SHOTS && (await page.screenshot({ path: `${SHOTS}/${name}.png` }));
const login = async (user) => {
  await page.goto(BASE + "/login", { waitUntil: "networkidle0" });
  await page.type('input[name="username"]', user);
  await page.type('input[name="password"]', "password123");
  await page.keyboard.press("Enter");
  await page.waitForFunction(() => location.pathname !== "/login", { timeout: 15000 });
};
const api = (p) => page.evaluate(async (p) => (await fetch(p, { credentials: "include" })).json(), p);
const tab = (label) =>
  page.evaluate((l) => [...document.querySelectorAll('[role="radio"]')].find((e) => e.textContent === l)?.click(), label);
// --- the customer's task page ----------------------------------------------------------------
await login("alice");
let taskId = process.env.TASK;
if (!taskId) {
  for (const t of await api("/api/portal/customer/tasks")) {
    const u = await api(`/api/portal/customer/tasks/${t.id}/usage`);
    if (u.supported && u.points.some((p) => p.tunnel_out_bytes > 0 || p.tunnel_in_bytes > 0)) {
      taskId = t.id;
      break;
    }
  }
}
await go(`${BASE}/tasks/${taskId}`);
let body = await page.$eval("main#main", (m) => m.innerText);
check(
  "the task page has a Resource use card",
  /Resource use/.test(body) && /Peak [\d.]+ of [\d.]+ cores/.test(body),
  body.match(/Peak [^\n]*/)?.[0]
);
const cpuInfo = await chartInfo(page);
check("the CPU chart has points", cpuInfo.points >= 2, `${cpuInfo.points} points`);
let tip = await hoverFind(page, (t) => !/No reading/.test(t));
check("hovering a moment says how many cores the task used", /CPU used/.test(tip) && /cores/.test(tip), tip.replace(/\n/g, " | "));
await shot("task-resource-cpu");
await tab("Memory");
await sleep(500);
tip = await hoverFind(page, (t) => !/No reading/.test(t));
check("memory shows the task's memory against what it asked for", /Memory used/.test(tip) && /MB/.test(tip), tip.replace(/\n/g, " | "));
await tab("Network");
await sleep(500);
tip = await hoverFind(page, (t) => !/No reading/.test(t));
check(
  "the network tab shows tunnel traffic in both directions",
  /Sent by the task/.test(tip) && /Received by the task/.test(tip) && /MB/.test(tip),
  tip.replace(/\n/g, " | ")
);
await shot("task-resource-network");
await tab("Gateway");
await sleep(500);
body = await page.$eval("main#main", (m) => m.innerText);
check(
  "the gateway tab has a line per gateway, named, with what it moved",
  /exactness · [\d.]+ (B|kB|MB)/.test(body) || /gateway · [\d.]+ (B|kB|MB)/.test(body),
  body.match(/[^\n]* · [\d.]+ (B|kB|MB)/)?.[0]
);
await shot("task-resource-gateway");

// A task that never started has no card (nothing to show yet).
const queued = (await api("/api/portal/customer/tasks")).find((t) => t.state === "queued" || t.state === "reserved");
if (queued) {
  await go(`${BASE}/tasks/${queued.id}`);
  check("a task that hasn't started has no empty chart", !/Resource use/.test(await page.$eval("main#main", (m) => m.innerText)));
}

// --- the provider's machine page: Network tab ---------------------------------------------------
// A fresh browser context, so the customer's session and its open event stream don't carry over.
await page.close();
page = await (await browser.createBrowserContext()).newPage();
await page.setViewport({ width: 1440, height: 1100 });
page.on("pageerror", (e) => errors.push(String(e)));
await login("bob");
const nodes = await api("/api/portal/provider/nodes");
const node = nodes.find((n) => n.connected) ?? nodes[0];
await go(`${BASE}/machines/${node.id}`);
await tab("Network");
await sleep(600);
tip = await hoverFind(page, (t) => !/No reading/.test(t));
check(
  "the machine's Network tab lists which task moved the data",
  /tsk_/.test(tip) && /MB/.test(tip) && /Out of the tasks/.test(tip),
  tip.replace(/\n/g, " | ")
);
await shot("machine-network");

check("no uncaught page errors", errors.length === 0, errors.join(" | "));
await browser.close();
const bad = results.filter((r) => !r.ok);
console.log(`\n${results.length - bad.length}/${results.length} checks passed`);
process.exit(bad.length ? 1 : 0);
