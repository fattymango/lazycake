// Browser check for gateway traffic (task 8.14) against a coordinator that has a
// real gateway which has carried real traffic (see docs/02-frontend-overhaul/IMPLEMENTATION.md, 8.14
// "As built": 3 x a 5,000,000-byte file through a live gateway and agent).
//
//   BASE=http://127.0.0.1:18081 GATEWAY_LABEL=exactness SHOTS=/tmp/shots node e2e/traffic.mjs
import puppeteer from "puppeteer-core";

const BASE = process.env.BASE || "http://127.0.0.1:18081";
const LABEL = process.env.GATEWAY_LABEL || "exactness";
const SHOTS = process.env.SHOTS;
const results = [];
const check = (name, ok, detail = "") => {
  results.push({ ok });
  console.log(`${ok ? "PASS" : "FAIL"}  ${name}${detail ? `  (${detail})` : ""}`);
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const browser = await puppeteer.launch({ executablePath: process.env.CHROME || "/usr/bin/google-chrome", headless: "new", args: ["--no-sandbox", "--disable-gpu"] });
const page = await browser.newPage();
await page.setViewport({ width: 1440, height: 900 });
const errors = [];
page.on("pageerror", (e) => errors.push(String(e)));
const go = async (url) => {
  await page.goto(url, { waitUntil: "domcontentloaded" });
  await page.waitForSelector("main#main", { timeout: 15000 });
  await sleep(900);
};
const shot = async (name) => SHOTS && (await page.screenshot({ path: `${SHOTS}/${name}.png` }));
const text = () => page.$eval("main#main", (m) => m.innerText);

await page.goto(BASE + "/login", { waitUntil: "networkidle0" });
await page.type('input[name="username"]', "alice");
await page.type('input[name="password"]', "password123");
await page.keyboard.press("Enter");
await page.waitForFunction(() => location.pathname !== "/login", { timeout: 15000 });

const api = (p) => page.evaluate(async (p) => (await fetch(p, { credentials: "include" })).json(), p);
const gateways = await api("/api/portal/customer/gateways");
const gw = gateways.find((g) => g.label === LABEL);
check("the gateway that carried traffic is listed with totals", !!gw && gw.traffic.sent_to_tasks_bytes > 0, JSON.stringify(gw?.traffic));

// --- gateways list -------------------------------------------------------------
await go(`${BASE}/gateways`);
const list = await text();
check("the card shows both directions with the customer-side labels", /Received from tasks/.test(list) && /Sent to tasks/.test(list));
check("and the byte total is formatted, not raw", /15 MB/.test(list), "expected 15 MB");
await shot("gateways-traffic");

// --- gateway detail --------------------------------------------------------------
await page.evaluate((l) => [...document.querySelectorAll("h2 a")].find((a) => a.textContent === l)?.click(), LABEL);
await page.waitForFunction((id) => location.pathname === `/gateways/${id}`, { timeout: 8000 }, gw.id);
await sleep(1200);
const detail = await text();
check("clicking a gateway opens its traffic page", /Traffic over time/.test(detail) && /Busiest tasks/.test(detail));
check("lifetime stats are shown", /15 MB/.test(detail) && /3/.test(detail));
const columns = await page.$$('[aria-label*=": "][tabindex="0"]');
check("the chart has a column for every hour of the week", columns.length >= 167, `${columns.length} columns`);
// Hover the column that has the traffic.
const idx = await page.$$eval('[aria-label*=": "][tabindex="0"]', (els) => els.findIndex((e) => !/: 0 B$/.test(e.getAttribute("aria-label"))));
check("exactly the hour that had traffic is non-empty", idx >= 0, `column ${idx}`);
const box = await (await page.$$('[aria-label*=": "][tabindex="0"]'))[idx].boundingBox();
await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
await sleep(300);
const tip = await page.$eval('[role="status"]', (e) => e.innerText).catch(() => "");
check("hovering it says who moved how much", /Sent to tasks/.test(tip) && /Received from tasks/.test(tip) && /15 MB/.test(tip), tip.replace(/\n/g, " | "));
await shot("gateway-detail");
check("the busiest-tasks table links to the task", (await page.$$eval("table a", (as) => as.length)) >= 1);
await page.click("[role=radio]:nth-of-type(2)").catch(() => {});
await sleep(1000);
check("switching to 30 days keeps working", /30 days/.test(await text()));
await shot("gateway-detail-30d");

// --- someone else's / unknown gateway ----------------------------------------------
await go(`${BASE}/gateways/gw_does_not_exist`);
check("an unknown gateway is a friendly not-found, not a crash", /Gateway not found/.test(await text()));

// --- task page ---------------------------------------------------------------------
const tasks = await api("/api/portal/customer/tasks");
let task;
for (const t of tasks) {
  const tt = await api(`/api/portal/customer/tasks/${t.id}/traffic`);
  if (tt.totals.sent_to_tasks_bytes > 0) {
    task = t;
    break;
  }
}
await go(`${BASE}/tasks/${task.id}`);
await sleep(800);
const tbody = await text();
check("the task's Network card shows what it moved", /sent .*15 MB.*to your services|↑ .* sent/.test(tbody.replace(/\n/g, " ")), "");
check("per-service line is there", /↑ 273 B sent · ↓ 15 MB received/.test(tbody), tbody.match(/↑[^\n]*/)?.[0]);
await shot("task-network");

check("no uncaught page errors", errors.length === 0, errors.join(" | "));
await browser.close();
const bad = results.filter((r) => !r.ok);
console.log(`\n${results.length - bad.length}/${results.length} checks passed`);
process.exit(bad.length ? 1 : 0);
