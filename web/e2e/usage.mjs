// Browser check for machine usage (task 8.15): a provider's machine page against a coordinator
// whose agent is reporting usage and has run benchmark tasks (e.g. lcbench at 0.5 cores / 128 MB).
//
//   BASE=http://127.0.0.1:18081 SHOTS=/tmp/shots node e2e/usage.mjs
import puppeteer from "puppeteer-core";

const BASE = process.env.BASE || "http://127.0.0.1:18081";
const SHOTS = process.env.SHOTS;
const results = [];
const check = (name, ok, detail = "") => {
  results.push({ ok });
  console.log(`${ok ? "PASS" : "FAIL"}  ${name}${detail ? `  (${detail})` : ""}`);
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const browser = await puppeteer.launch({ executablePath: process.env.CHROME || "/usr/bin/google-chrome", headless: "new", args: ["--no-sandbox", "--disable-gpu"] });
const page = await browser.newPage();
await page.setViewport({ width: 1440, height: 1000 });
const errors = [];
page.on("pageerror", (e) => errors.push(String(e)));
const go = async (url) => {
  await page.goto(url, { waitUntil: "domcontentloaded" });
  await page.waitForSelector("main#main", { timeout: 15000 });
  await sleep(1200);
};
const shot = async (name) => SHOTS && (await page.screenshot({ path: `${SHOTS}/${name}.png` }));
const text = () => page.$eval("main#main", (m) => m.innerText);

await page.goto(BASE + "/login", { waitUntil: "networkidle0" });
await page.type('input[name="username"]', "bob");
await page.type('input[name="password"]', "password123");
await page.keyboard.press("Enter");
await page.waitForFunction(() => location.pathname !== "/login", { timeout: 15000 });

const api = (p) => page.evaluate(async (p) => (await fetch(p, { credentials: "include" })).json(), p);
const nodes = await api("/api/portal/provider/nodes");
const node = nodes.find((n) => n.connected) ?? nodes[0];

// --- overview: a trend on the machine card -------------------------------------------
await go(`${BASE}/`);
const overview = await text();
check("the machine card shows CPU in use over 24 h", /CPU in use, 24 h/.test(overview));
await shot("provider-overview-usage");

// --- machine page ----------------------------------------------------------------------
await go(`${BASE}/machines/${node.id}`);
const body = await text();
check("the Usage card is there with live gauges", /Usage/.test(body) && /CPU used by tasks/.test(body) && /Memory used by tasks/.test(body));
check("gauges read in the provider's terms, 'x of N offered cores'", /\d(\.\d+)? of 2\b/.test(body), body.match(/[\d.]+ of 2\b/)?.[0]);
check("it says the numbers don't affect billing or trust", /doesn't affect billing or trust/.test(body));
const cols = await page.$$('[aria-label*=": "][tabindex="0"]');
check("the history chart has a column per five minutes of a day", cols.length === 288, `${cols.length}`);
const labels = await page.$$eval('[aria-label*=": "][tabindex="0"]', (els) => els.map((e) => e.getAttribute("aria-label")));
const offline = labels.filter((l) => /offline/.test(l)).length;
check("periods with no reading are gaps, not zeros", offline > 0 && offline < labels.length, `${offline} gap columns`);

// Hover the newest column that has data and read who used what.
const idx = labels.map((l, i) => [l, i]).filter(([l]) => !/offline|: 0 cores/.test(l)).map(([, i]) => i).pop();
check("some column has task usage", idx !== undefined, `column ${idx}`);
const box = await (await page.$$('[aria-label*=": "][tabindex="0"]'))[idx].boundingBox();
await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
await sleep(400);
const tip = await page.$eval('[role="status"]', (e) => e.innerText).catch(() => "");
check("hovering a moment lists each task and the machine total", /tsk_/.test(tip) && /cores/.test(tip) && /of 2 cores/.test(tip), tip.replace(/\n/g, " | "));
await shot("machine-usage-cpu");

// Memory tab and 7 days.
await page.evaluate(() => [...document.querySelectorAll('[role="radio"]')].find((e) => e.textContent === "Memory")?.click());
await sleep(500);
const memCols = await page.$$('[aria-label*=": "][tabindex="0"]');
check("memory tab charts the same period", memCols.length === 288);
await shot("machine-usage-memory");
await page.evaluate(() => [...document.querySelectorAll('[role="radio"]')].find((e) => e.textContent === "7 days")?.click());
await sleep(1200);
const weekCols = await page.$$('[aria-label*=": "][tabindex="0"]');
check("7 days switches to one column per hour", weekCols.length === 168, `${weekCols.length}`);
await shot("machine-usage-7d");

// --- hover on a task row ---------------------------------------------------------------------
const rows = await page.$$("table tbody tr");
check("the machine's task table is there", rows.length > 0);
let hoverText = "";
for (const row of rows) {
  const b = await row.boundingBox();
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2);
  await sleep(250);
  hoverText = await page.$eval('[role="tooltip"]', (e) => e.innerText).catch(() => "");
  if (/What this task used/.test(hoverText)) break;
}
check("hovering a task row shows what it used", /CPU time/.test(hoverText) && /Peak memory/.test(hoverText) && /Tunnel traffic/.test(hoverText), hoverText.replace(/\n/g, " | "));
await shot("machine-task-hover");

check("no uncaught page errors", errors.length === 0, errors.join(" | "));
await browser.close();
const bad = results.filter((r) => !r.ok);
console.log(`\n${results.length - bad.length}/${results.length} checks passed`);
process.exit(bad.length ? 1 : 0);
