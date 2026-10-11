// Browser check for the version indicator and guided update (task 8.16). Needs a coordinator built as v0.4.0 and demo data where:
// lab-laptop never reported a version, pi-cluster-node-3 runs v0.3.13 (behind), kassab-workstation runs v0.4.0-3-g... (current),
// and the gateways internal-files (never reported), staging-api-gateway-eu-west (v0.3.0), production-postgres (v0.4.0).
//
//   BASE=http://127.0.0.1:18081 SHOTS=/tmp/shots node e2e/update.mjs
import puppeteer from "puppeteer-core";

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
const errors = [];
const newPage = async () => {
  const ctx = await browser.createBrowserContext();
  const p = await ctx.newPage();
  await p.setViewport({ width: 1440, height: 1100 });
  p.on("pageerror", (e) => errors.push(String(e)));
  return p;
};
const login = async (p, user) => {
  await p.goto(BASE + "/login", { waitUntil: "networkidle0" });
  await p.type('input[name="username"]', user);
  await p.type('input[name="password"]', "password123");
  await p.keyboard.press("Enter");
  await p.waitForFunction(() => location.pathname !== "/login", { timeout: 15000 });
};
const go = async (p, path) => {
  await p.goto(BASE + path, { waitUntil: "domcontentloaded" });
  await p.waitForSelector("main#main", { timeout: 15000 });
  await sleep(1000);
};
const text = (p) => p.$eval("main#main", (m) => m.innerText);
const clickByText = (p, re) =>
  p.evaluate(
    (src) =>
      [...document.querySelectorAll("button, a")].find((e) => new RegExp(src).test(e.getAttribute("aria-label") || e.textContent))?.click(),
    re.source
  );

// --- provider: machines -------------------------------------------------------------------------------------
const bob = await newPage();
await login(bob, "bob");
const nodes = await bob.evaluate(async () => (await fetch("/api/portal/provider/nodes", { credentials: "include" })).json());
const byName = Object.fromEntries(nodes.map((n) => [n.hostname, n]));
check("the API says a machine that never reported is unknown", byName["lab-laptop"]?.update_state === "unknown");
check(
  "a machine behind the coordinator is outdated",
  byName["pi-cluster-node-3"]?.update_state === "outdated",
  byName["pi-cluster-node-3"]?.version
);
check(
  "a machine on the coordinator's release (a few commits past it) is current",
  byName["kassab-workstation-ubuntu-24-04"]?.update_state === "current"
);

await go(bob, "/");
const overview = await text(bob);
check("machine cards flag the outdated and unknown ones", /Update available/.test(overview) && /Version unknown/.test(overview));

await go(bob, `/machines/${byName["pi-cluster-node-3"].id}`);
let body = await text(bob);
check("an outdated machine's page shows its version and an update badge", /Update available/.test(body) && /v0\.3\.13/.test(body));
await clickByText(bob, /Update available: how to update/);
await bob.waitForSelector('[role="dialog"]', { timeout: 5000 });
let dialog = await bob.$eval('[role="dialog"]', (d) => d.innerText);
check(
  "the badge opens a guided update with both versions",
  /Running/.test(dialog) && /v0\.3\.13/.test(dialog) && /Newest/.test(dialog) && /v0\.4\.0/.test(dialog)
);
check(
  "with the commands, for an agent",
  /podman pull docker\.io\/fattymango\/lazycake-agent:latest/.test(dialog) && /podman inspect lazycake-agent/.test(dialog)
);
check("and says LazyCake never pushes code to the machine", /never pushes code/.test(dialog));
await sleep(500); // let the dialog finish fading in
if (SHOTS) await bob.screenshot({ path: `${SHOTS}/update-dialog-agent.png` });
await bob.keyboard.press("Escape");

await go(bob, `/machines/${byName["kassab-workstation-ubuntu-24-04"].id}`);
body = await text(bob);
check(
  "a current machine shows its version and no update badge",
  /v0\.4\.0-3-g/.test(body) && !/Update available|Version unknown/.test(body)
);

await go(bob, `/machines/${byName["lab-laptop"].id}`);
body = await text(bob);
check("a machine that never reported says so, and flags it", /not reported/.test(body) && /Version unknown/.test(body));

// --- customer: gateways -----------------------------------------------------------------------------------
const alice = await newPage();
await login(alice, "alice");
await go(alice, "/gateways");
body = await text(alice);
const cards = await alice.$$eval("main#main ul > li", (lis) => lis.map((l) => l.innerText));
const card = (name) => cards.find((c) => c.includes(name)) || "";
check("an outdated gateway is flagged", /Update available/.test(card("staging-api-gateway")));
check("one that never reported is flagged", /Version unknown/.test(card("internal-files")));
check("a current gateway has no badge", !/Update available|Version unknown/.test(card("production-postgres")));
await alice.evaluate(() =>
  [...document.querySelectorAll("main#main ul > li")]
    .find((l) => l.innerText.includes("staging-api-gateway"))
    .querySelector('button[aria-label^="Update available"]')
    .click()
);
await alice.waitForSelector('[role="dialog"]', { timeout: 5000 });
dialog = await alice.$eval('[role="dialog"]', (d) => d.innerText);
check(
  "a gateway's guided update names the gateway image and container",
  /lazycake-gateway:latest/.test(dialog) && /podman inspect lazycake-gateway/.test(dialog) && /v0\.3\.0/.test(dialog)
);
await sleep(500);
if (SHOTS) await alice.screenshot({ path: `${SHOTS}/update-dialog-gateway.png` });
await alice.keyboard.press("Escape");

check("no uncaught page errors", errors.length === 0, errors.join(" | "));
await browser.close();
const bad = results.filter((r) => !r.ok);
console.log(`\n${results.length - bad.length}/${results.length} checks passed`);
process.exit(bad.length ? 1 : 0);
