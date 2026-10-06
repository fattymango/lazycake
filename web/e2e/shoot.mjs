// Screenshots every portal page at several viewport widths and themes and
// audits each for layout overflow. Drives the system Chrome through
// puppeteer-core, logging in through the real login form.
//
//   BASE=http://localhost:5173 node e2e/shoot.mjs --out /tmp/shots
//   node e2e/shoot.mjs --role provider --widths 390,1440 --themes dark
//
// Credentials default to the local demo accounts created by seed.py's
// instructions (alice/bob, password123). Exit code 1 if any page overflows.
import puppeteer from "puppeteer-core";
import { mkdirSync } from "node:fs";

const args = Object.fromEntries(
  process.argv.slice(2).reduce((acc, a, i, all) => (a.startsWith("--") ? [...acc, [a.slice(2), all[i + 1]]] : acc), [])
);
const BASE = process.env.BASE || "http://localhost:5173";
const OUT = args.out || "/tmp/lazycake-shots";
const WIDTHS = (args.widths || "390,820,1440").split(",").map(Number);
const THEMES = (args.themes || "dark,light").split(",");
const ROLES = (args.role || "customer,provider").split(",");
const ONLY = args.only ? new RegExp(args.only) : null;
const ACCOUNTS = { customer: ["alice", "password123"], provider: ["bob", "password123"] };
const THEME_KEY = "lc-theme";
mkdirSync(OUT, { recursive: true });

const browser = await puppeteer.launch({
  executablePath: process.env.CHROME || "/usr/bin/google-chrome",
  headless: "new",
  args: ["--no-sandbox", "--disable-gpu", "--hide-scrollbars"],
});

const problems = [];
const consoleErrors = [];

// A page may still be navigating (a client-side redirect, a late reload) when we
// measure it; retry instead of crashing the whole run.
async function safeEvaluate(page, fn, ...args) {
  for (let i = 0; ; i++) {
    try {
      return await page.evaluate(fn, ...args);
    } catch (err) {
      if (i >= 4 || !/context was destroyed|navigation/i.test(String(err))) throw err;
      await new Promise((r) => setTimeout(r, 600));
    }
  }
}

async function api(page, path) {
  return page.evaluate(async (p) => (await fetch(p, { credentials: "include" })).json(), path);
}

async function login(page, role) {
  const [user, pass] = ACCOUNTS[role];
  await page.goto(BASE + "/login", { waitUntil: "domcontentloaded" });
  await page.waitForSelector('input[name="username"]');
  await page.type('input[autocomplete="username"], input[name="username"]', user);
  await page.type('input[type="password"]', pass);
  await page.keyboard.press("Enter");
  await page.waitForFunction(() => location.pathname !== "/login", { timeout: 15000 });
  await page.goto(BASE + "/", { waitUntil: "domcontentloaded" });
  await page.waitForSelector("main#main");
}

async function routesFor(page, role) {
  if (role === "customer") {
    const tasks = await api(page, "/api/portal/customer/tasks");
    const pick = (s) => tasks.find((t) => t.state === s)?.id;
    return [
      ["dashboard", "/"],
      ["tasks", "/tasks"],
      ["submit", "/tasks/new"],
      ["task-running", `/tasks/${pick("running")}`],
      ["task-failed", `/tasks/${pick("failed")}`],
      ["task-queued", `/tasks/${pick("queued")}`],
      ["task-not-found", "/tasks/tsk_does_not_exist"],
      ["gateways", "/gateways"],
      ["billing", "/billing"],
      ["not-found", "/this/page/does/not/exist"],
    ];
  }
  const nodes = await api(page, "/api/portal/provider/nodes");
  return [
    ["dashboard", "/"],
    ["add-machine", "/machines/add"],
    ["machine", `/machines/${nodes[0]?.id}`],
    ["machine-offline", `/machines/${(nodes.find((n) => !n.connected) || nodes[0])?.id}`],
    ["earnings", "/earnings"],
  ];
}

// Elements that poke outside their container or the viewport, and long
// unbroken strings that were not truncated.
function auditScript() {
  const vw = document.documentElement.clientWidth;
  const bad = [];
  const rootOverflow = document.documentElement.scrollWidth - vw;
  if (rootOverflow > 1) bad.push(`page scrolls horizontally by ${rootOverflow}px`);
  for (const el of document.querySelectorAll("body *")) {
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.visibility === "hidden" || el.closest("[data-overflow-ok]")) continue;
    const r = el.getBoundingClientRect();
    if (r.width === 0 || r.height === 0) continue;
    if (r.right > vw + 1 && !el.closest("[data-scroll-x]")) {
      bad.push(`${el.tagName.toLowerCase()}.${String(el.className).slice(0, 60)} extends ${Math.round(r.right - vw)}px past the viewport: "${(el.textContent || "").trim().slice(0, 50)}"`);
    }
    const clipsText = el.children.length === 0 && el.scrollWidth > el.clientWidth + 1 && cs.overflow === "visible" && cs.display !== "inline";
    if (clipsText && !el.closest("[data-scroll-x]")) {
      bad.push(`text spills out of ${el.tagName.toLowerCase()}.${String(el.className).slice(0, 50)} (${el.scrollWidth}>${el.clientWidth}): "${(el.textContent || "").trim().slice(0, 50)}"`);
    }
  }
  return [...new Set(bad)].slice(0, 12);
}

for (const role of ROLES) {
  for (const theme of THEMES) {
    for (const width of WIDTHS) {
      // A fresh browser context per pass: contexts share nothing, so a previous
      // pass's session cookie can't turn /login into an instant redirect.
      const context = await browser.createBrowserContext();
      const page = await context.newPage();
      page.on("console", (m) => m.type() === "error" && !/401 \(Unauthorized\)/.test(m.text()) && consoleErrors.push(`[${role}/${theme}/${width}] ${m.text().slice(0, 160)}`));
      page.on("pageerror", (e) => consoleErrors.push(`[${role}/${theme}/${width}] pageerror: ${String(e).slice(0, 160)}`));
      await page.setViewport({ width, height: width < 600 ? 800 : 900, deviceScaleFactor: 1 });
      await page.emulateMediaFeatures([{ name: "prefers-color-scheme", value: theme }]);
      await page.evaluateOnNewDocument((k, t) => localStorage.setItem(k, t), THEME_KEY, theme);
      await login(page, role);
      for (const [name, route] of await routesFor(page, role)) {
        const label = `${role}-${name}`;
        if (ONLY && !ONLY.test(label)) continue;
        await page.goto(BASE + route, { waitUntil: "domcontentloaded", timeout: 20000 }).catch(() => {});
        await page.waitForSelector("main#main", { timeout: 10000 }).catch(() => {});
        await new Promise((r) => setTimeout(r, 900)); // let data load and animations settle
        const file = `${OUT}/${label}.${theme}.${width}.png`;
        await page.screenshot({ path: file, fullPage: true });
        const bad = await safeEvaluate(page, auditScript);
        if (bad.length) problems.push({ page: `${label} ${theme} ${width}px`, bad });
      }
      await context.close();
    }
  }
}
await browser.close();

if (consoleErrors.length) console.log("\nconsole errors:\n  " + [...new Set(consoleErrors)].slice(0, 20).join("\n  "));
if (problems.length) {
  console.log("\nlayout problems:");
  for (const p of problems) console.log(`  ${p.page}\n    - ` + p.bad.join("\n    - "));
  console.log(`\n${problems.length} page view(s) with layout problems. Screenshots in ${OUT}`);
  process.exit(1);
}
console.log(`\nno layout problems. Screenshots in ${OUT}`);
