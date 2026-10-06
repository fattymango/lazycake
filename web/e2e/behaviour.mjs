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

const browser = await puppeteer.launch({ executablePath: process.env.CHROME || "/usr/bin/google-chrome", headless: "new", args: ["--no-sandbox", "--disable-gpu"] });
const page = await browser.newPage();
await page.setViewport({ width: 1440, height: 900 });

// Count every request to a task log stream.
const logRequests = [];
page.on("request", (r) => /\/tasks\/[^/]+\/logs/.test(r.url()) && logRequests.push(r.url()));
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
await page.goto(`${BASE}/tasks/${failed.id}`, { waitUntil: "networkidle2" });
await page.waitForSelector('[role="log"]');
const before = (await page.$eval("h1", (h) => h.textContent)) || "";
check("deep link renders the task page", before.includes("tsk_"), before.slice(0, 40));

redirects.length = 0;
const reloaded = await page.reload({ waitUntil: "networkidle2" });
check("refreshing the deep link returns 200, not a redirect loop", reloaded.status() === 200, `status ${reloaded.status()}`);
check("no redirects were followed", redirects.filter((r) => r.includes("/tasks/")).length === 0, redirects.join(", "));
await page.waitForSelector('[role="log"]');
const after = (await page.$eval("h1", (h) => h.textContent)) || "";
check("after refresh the same task is shown", after === before, after.slice(0, 40));

for (const path of ["/tasks", "/gateways", "/billing", "/tasks/new"]) {
  const r = await page.goto(BASE + path, { waitUntil: "networkidle2" });
  check(`direct load of ${path} is 200`, r.status() === 200, `status ${r.status()}`);
}

const nope = await page.goto(BASE + "/this/does/not/exist", { waitUntil: "networkidle2" });
const nopeText = await page.$eval("body", (b) => b.innerText);
check("an unknown route shows the app's 404 page", nope.status() === 200 && /Page not found/.test(nopeText), `status ${nope.status()}`);

await page.goto(BASE + "/tasks/tsk_does_not_exist", { waitUntil: "networkidle2" });
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
await page.goto(`${BASE}/tasks/${failed.id}`, { waitUntil: "networkidle2" });
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
await page.goto(`${BASE}/tasks/${running.id}`, { waitUntil: "networkidle2" });
await page.waitForSelector('[role="log"]');
await sleep(3000);
const runLines = await lineCount();
const runStatus = await page.$eval('[role="log"]', (el) => el.parentElement.parentElement.textContent);
check("a running task's stream is live", /Live/.test(runStatus));
await sleep(5000);
check("a live stream does not duplicate lines", (await lineCount()) === runLines, `${runLines} lines`);
const seqs = await page.$$eval('[role="log"] > div[class*="flex"] > span:first-child', (els) => els.map((e) => Number(e.textContent)));
check("line numbers are unique and ascending", seqs.every((s, i) => i === 0 || s > seqs[i - 1]), `${seqs.length} lines`);

await browser.close();
const failedChecks = results.filter((r) => !r.ok);
console.log(`\n${results.length - failedChecks.length}/${results.length} checks passed`);
process.exit(failedChecks.length ? 1 : 0);
