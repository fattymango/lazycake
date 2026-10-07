// Behavioural browser tests for bugs that only show up in a real browser
// against the real production build served by the coordinator (not the Vite
// dev server): refreshing a deep link, and a finished task's log repeating.
//
//   BASE=http://127.0.0.1:18081 node e2e/behaviour.mjs
//
// Needs a coordinator with the seed data from e2e/seed.py and the demo
// accounts (alice/password123). Exits 1 if any check fails.
import puppeteer from "puppeteer-core";

const BASE = process.env.BASE || "http://127.0.0.1:18081";
const results = [];
const check = (name, ok, detail = "") => {
  results.push({ name, ok, detail });
  console.log(`${ok ? "PASS" : "FAIL"}  ${name}${detail ? `  (${detail})` : ""}`);
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
// "Network idle" never happens while a live-update stream is open, so wait for
// the app's main content to render instead.
let page;
async function go(url) {
  let res;
  try {
    res = await page.goto(url, { waitUntil: "domcontentloaded", timeout: 12000 });
  } catch (err) {
    const alive = await Promise.race([page.evaluate(() => "alive"), sleep(2500).then(() => "FROZEN")]);
    console.log(`navigation to ${url} stalled; page ${alive}; now at ${page.url()}; pending=${[...pending].join(" | ")}`);
    throw err;
  }
  await page.waitForSelector("main#main", { timeout: 15000 });
  await sleep(700);
  return res;
}

const browser = await puppeteer.launch({
  executablePath: process.env.CHROME || "/usr/bin/google-chrome",
  headless: "new",
  args: ["--no-sandbox", "--disable-gpu"],
});
page = await browser.newPage();
await page.setViewport({ width: 1440, height: 900 });

// Count every request to a task log stream.
const logRequests = [];
page.on("request", (r) => /\/tasks\/[^/]+\/logs/.test(r.url()) && logRequests.push(r.url()));
const pending = new Set();
page.on("request", (r) => pending.add(r.url().replace(BASE, "")));
page.on("requestfinished", (r) => pending.delete(r.url().replace(BASE, "")));
page.on("requestfailed", (r) => pending.delete(r.url().replace(BASE, "")));
const redirects = [];
page.on("response", (r) => r.status() >= 300 && r.status() < 400 && redirects.push(`${r.status()} ${r.url()}`));

// --- sign in -----------------------------------------------------------------
await page.goto(BASE + "/login", { waitUntil: "networkidle0" });
await page.type('input[name="username"]', "alice");
await page.type('input[name="password"]', "password123");
await page.keyboard.press("Enter");
await page.waitForFunction(() => location.pathname !== "/login", { timeout: 15000 });

const tasks = await page.evaluate(async () => (await fetch("/api/portal/customer/tasks", { credentials: "include" })).json());
const failed = tasks.find((t) => t.state === "failed" && t.exit_reason === "exited");
const running = tasks.find((t) => t.state === "running");
const lineCount = () => page.$$eval('[role="log"] > div[class*="flex"]', (els) => els.length);

// --- bug 1: refreshing a deep link -------------------------------------------
await go(`${BASE}/tasks/${failed.id}`);
await page.waitForSelector('[role="log"]');
const before = (await page.$eval("h1", (h) => h.textContent)) || "";
check("deep link renders the task page", before.includes("tsk_"), before.slice(0, 40));

redirects.length = 0;
const reloaded = await page.reload({ waitUntil: "domcontentloaded" });
check("refreshing the deep link returns 200, not a redirect loop", reloaded.status() === 200, `status ${reloaded.status()}`);
check("no redirects were followed", redirects.filter((r) => r.includes("/tasks/")).length === 0, redirects.join(", "));
await page.waitForSelector('[role="log"]');
const after = (await page.$eval("h1", (h) => h.textContent)) || "";
check("after refresh the same task is shown", after === before, after.slice(0, 40));

for (const path of ["/tasks", "/gateways", "/billing", "/tasks/new"]) {
  const r = await go(BASE + path);
  check(`direct load of ${path} is 200`, r.status() === 200, `status ${r.status()}`);
}

const nope = await go(BASE + "/this/does/not/exist");
const nopeText = await page.$eval("body", (b) => b.innerText);
check("an unknown route shows the app's 404 page", nope.status() === 200 && /Page not found/.test(nopeText), `status ${nope.status()}`);

await go(BASE + "/tasks/tsk_does_not_exist");
const missingText = await page.$eval("body", (b) => b.innerText);
check("a task that doesn't exist shows a designed not-found", /Task not found/.test(missingText));

const stale = await page.evaluate(async () => (await fetch("/assets/index-STALE.js")).status);
check("a missing asset is a real 404 (not HTML served as JS)", stale === 404, `status ${stale}`);
const apiTypo = await page.evaluate(async () => {
  const r = await fetch("/api/portal/nope");
  return [r.status, (r.headers.get("content-type") || "").slice(0, 20)];
});
check("an unknown API path is a JSON 404", apiTypo[0] === 404 && apiTypo[1].includes("json"), apiTypo.join(" "));

// --- bug 2: a finished task's log must not repeat ----------------------------
logRequests.length = 0;
await go(`${BASE}/tasks/${failed.id}`);
await page.waitForSelector('[role="log"]');
await sleep(1500);
const first = await lineCount();
check("the failed task's log shows its 17 lines", first === 17, `${first} lines`);
await sleep(11000); // the old bug re-appended the whole log every few seconds
const later = await lineCount();
check("the log does not repeat after the task has finished", later === first, `${first} -> ${later} lines`);
check("the log stream was opened once and not reconnected", logRequests.length === 1, `${logRequests.length} request(s)`);
const status = await page.$eval('[role="log"]', (el) => el.parentElement.parentElement.textContent);
check("the viewer says the stream finished", /Finished/.test(status));

// --- running task: live stream stays open without duplicating -----------------
logRequests.length = 0;
await go(`${BASE}/tasks/${running.id}`);
await page.waitForSelector('[role="log"]');
await sleep(3000);
const runLines = await lineCount();
const runStatus = await page.$eval('[role="log"]', (el) => el.parentElement.parentElement.textContent);
check("a running task's stream is live", /Live/.test(runStatus));
await sleep(5000);
check("a live stream does not duplicate lines", (await lineCount()) === runLines, `${runLines} lines`);
const seqs = await page.$$eval('[role="log"] > div[class*="flex"] > span:first-child', (els) => els.map((e) => Number(e.textContent)));
check(
  "line numbers are unique and ascending",
  seqs.every((s, i) => i === 0 || s > seqs[i - 1]),
  `${seqs.length} lines`
);

// --- stopping a task (task 8.12) -------------------------------------------------
// A queued task is stopped on the spot and costs nothing. Resources no node can
// satisfy keep it queued, so this needs no agent.
const balanceBefore = await page.evaluate(
  async () => (await (await fetch("/api/portal/customer/me", { credentials: "include" })).json()).balance_micros
);
const queued = await page.evaluate(async () => {
  const r = await fetch("/api/portal/customer/tasks", {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      image: "docker.io/library/alpine@sha256:c64c687cbea9300178b30c95835354e34c4e4febc4badfe27102879de0483b5e",
      args: ["sleep", "60"],
      cores: 32,
      memory_mb: 65536,
      wall_timeout_s: 60,
    }),
  });
  return r.json();
});
check("a task nothing can run stays queued", queued.state === "queued", `${queued.state} ${queued.error || ""}`);
await go(`${BASE}/tasks/${queued.id}`);
await page.waitForFunction(() => [...document.querySelectorAll("button")].some((x) => /Stop task/.test(x.textContent)), { timeout: 10000 });
await page.evaluate(() => [...document.querySelectorAll("button")].find((x) => /Stop task/.test(x.textContent))?.click());
await page.waitForSelector('[role="dialog"]');
check("stopping asks for confirmation first", /Stop this task\?/.test(await page.$eval('[role="dialog"]', (d) => d.innerText)));
await page.evaluate(() => [...document.querySelectorAll('[role="dialog"] button')].find((x) => /Stop task/.test(x.textContent))?.click());
await page
  .waitForFunction(() => /Stopped by you/.test(document.body.innerText), { timeout: 8000 })
  .then(
    () => check("a queued task is stopped at once", true),
    () => check("a queued task is stopped at once", false, "never showed 'Stopped by you'")
  );
const stoppedText = await page.$eval("body", (b) => b.innerText);
check("it says nothing was charged", /nothing was charged/.test(stoppedText));
check("the label reads Stopped (not Cancelled)", /Stopped/.test(stoppedText) && !/\bCancelled\b/.test(stoppedText.split("Lifecycle")[0]));
check(
  "the Stop button is gone once it has finished",
  !(await page.evaluate(() => [...document.querySelectorAll("button")].some((x) => /^Stop task$/.test(x.textContent.trim()))))
);
const balanceAfter = await page.evaluate(
  async () => (await (await fetch("/api/portal/customer/me", { credentials: "include" })).json()).balance_micros
);
check("a stopped queued task costs nothing", balanceAfter === balanceBefore, `${balanceBefore} -> ${balanceAfter}`);
const again = await page.evaluate(
  async (id) => (await fetch(`/api/portal/customer/tasks/${id}/cancel`, { method: "POST", credentials: "include" })).status,
  queued.id
);
check("stopping it again is harmless", again === 200, `status ${again}`);
const fin = await page.evaluate(
  async (id) => (await fetch(`/api/portal/customer/tasks/${id}/cancel`, { method: "POST", credentials: "include" })).status,
  failed.id
);
check("a finished task can't be stopped", fin === 422, `status ${fin}`);

// --- gateway "Test connection" (task 8.13) --------------------------------------
// The seeded gateways have no process behind them, so they must read as not
// connected (red). The green/yellow/grey states need real gateways and are
// checked by hand and in the Go tests.
await go(`${BASE}/gateways`);
await page.waitForSelector("main#main ul > li");
const testButtons = await page.$$eval("button", (bs) => bs.filter((x) => /Test connection/.test(x.textContent)).length);
check("every gateway has a Test connection button", testButtons >= 1, `${testButtons} button(s)`);
await page.evaluate(() => [...document.querySelectorAll("button")].find((x) => /Test connection/.test(x.textContent))?.click());
await page.waitForSelector('[data-testid="gateway-test-result"]', { timeout: 15000 });
const gwStatus = await page.$eval('[data-testid="gateway-test-result"]', (e) => e.getAttribute("data-status"));
check("a gateway that isn't running tests as red", gwStatus === "red", gwStatus);
const gwText = await page.$eval('[data-testid="gateway-test-result"]', (e) => e.innerText);
check("and says what to do about it", /Gateway not connected/.test(gwText) && /running/.test(gwText));
check(
  "the button is throttled for a few seconds afterwards (frontend-only)",
  await page.evaluate(() => [...document.querySelectorAll("button")].some((x) => /Test again in \d+s/.test(x.textContent) && x.disabled))
);
await page.evaluate(() => document.querySelector('button[aria-label="What do the results mean?"]')?.click());
await page.waitForFunction(() => /What the results mean/.test(document.body.innerText), { timeout: 5000 }).catch(() => {});
const legendText = await page.$eval("body", (b) => b.innerText);
check(
  "the legend names every state",
  ["All services reachable", "Connected, but a service isn't reachable", "Gateway not connected", "Connected, services not verified"].every(
    (t) => legendText.includes(t)
  )
);
await page.keyboard.press("Escape");

// --- signing out clears the remembered page ----------------------------------
// Reported bug: log out on a customer's task page, sign in as a provider, and
// land on the customer's /tasks/<id> (which doesn't exist for a provider).
await go(`${BASE}/tasks/${failed.id}`);
await page.click('button[aria-label="Account menu"]');
await page.waitForSelector('[role="menuitem"]');
await page.evaluate(() => [...document.querySelectorAll('[role="menuitem"]')].find((e) => /Sign out/.test(e.textContent))?.click());
await page.waitForFunction(() => location.pathname === "/login", { timeout: 10000 });
check("signing out lands on the sign-in page", true);
await page.type('input[name="username"]', "bob");
await page.type('input[name="password"]', "password123");
await page.keyboard.press("Enter");
await page.waitForFunction(() => location.pathname !== "/login", { timeout: 15000 });
await page.waitForSelector("main#main");
const landed = await page.evaluate(() => location.pathname);
check("a different role signing in after logout starts at the overview, not the old page", landed === "/", `landed on ${landed}`);
const body = await page.$eval("body", (b) => b.innerText);
check("and sees the provider portal", /Provider/.test(body) && /Machines/.test(body));

await browser.close();
const failedChecks = results.filter((r) => !r.ok);
console.log(`\n${results.length - failedChecks.length}/${results.length} checks passed`);
process.exit(failedChecks.length ? 1 : 0);
