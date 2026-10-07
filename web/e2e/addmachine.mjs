// Browser check for the "Add a machine" page: the provider chooses CPU, memory, storage and network,
// the limits follow what the machine has (once told), and the install command carries the choice.
//
//   BASE=http://127.0.0.1:18081 SHOTS=/tmp/shots node e2e/addmachine.mjs
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
const page = await browser.newPage();
await page.setViewport({ width: 1440, height: 1500 });
const errors = [];
page.on("pageerror", (e) => errors.push(String(e)));
const shot = async (n) => SHOTS && (await page.screenshot({ path: `${SHOTS}/${n}.png` }));
const text = () => page.$eval("main#main", (m) => m.innerText);

await page.goto(BASE + "/login", { waitUntil: "networkidle0" });
await page.type('input[name="username"]', "bob");
await page.type('input[name="password"]', "password123");
await page.keyboard.press("Enter");
await page.waitForFunction(() => location.pathname !== "/login", { timeout: 15000 });
await page.goto(BASE + "/machines/add", { waitUntil: "domcontentloaded" });
await page.waitForSelector("main#main");
await sleep(800);

const val = (label) => page.$eval(`input[aria-label="${label} to lend"]`, (e) => e.value);
// Replace a controlled number input's value the way a person's typing would end up, firing React's input event.
const setVal = async (label, v) => {
  await page.$eval(
    `input[aria-label="${label} to lend"]`,
    (el, value) => {
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set;
      setter.call(el, String(value));
      el.dispatchEvent(new Event("input", { bubbles: true }));
    },
    v
  );
  await sleep(150);
};
const genButton = () =>
  page.evaluate(() => [...document.querySelectorAll("button")].find((b) => /Generate install command/.test(b.textContent))?.disabled);

let body = await text();
check(
  "the page asks how much CPU, memory, storage and network to lend",
  ["CPU", "Memory", "Storage", "Network"].every((l) => body.includes(l))
);
check(
  "each has a number field with a default",
  (await val("CPU")) === "2" && (await val("Memory")) === "2" && (await val("Storage")) === "8" && (await val("Network")) === "100"
);
check("each has a slider", (await page.$$('input[type="range"]')).length === 4);
check("it says the agent refuses an offer bigger than the machine", /refuses to start/.test(body));
check("without the machine's numbers it says the agent will do the check", /agent does that check itself/.test(body));

// Tell the page what the machine has.
await page.type('textarea[aria-label="Output of the capacity command"]', "cores=12 memory_mb=15314 disk_mb=441802 network_mbps=1000");
await sleep(300);
body = await text();
check(
  "pasting the capacity line is understood",
  /Got it: 12 cores, 14\.95 GB memory, 431\.44 GB free, 1000 Mbps link/.test(body),
  body.match(/Got it:[^\n]*/)?.[0]
);
check(
  "each limit now says what the machine has",
  /This machine has 12 cores/.test(body) && /This machine has 14\.95 GB/.test(body) && /a 1000 Mbps link/.test(body)
);
await shot("addmachine-limits");

// Offer more than the machine has, field by field.
await setVal("CPU", 13);
body = await text();
check("more cores than the machine has is flagged", /This machine only has 12 cores/.test(body));
check("and the install command can't be generated", (await genButton()) === true);
await setVal("CPU", 8);
await setVal("Memory", 16);
check("more memory than the machine has is flagged", /only has 14\.95 GB/.test(await text()));
await setVal("Memory", 8);
await setVal("Storage", 500);
check("more storage than is free is flagged", /only has 431\.44 GB free/.test(await text()));
await setVal("Storage", 100);
await setVal("Network", 2000);
check("a network offer above the link speed is flagged", /network link is 1000 Mbps/.test(await text()));
await shot("addmachine-errors");
await setVal("Network", 500);
check("with everything inside the limits, generating is allowed", (await genButton()) === false);

await page.evaluate(() => [...document.querySelectorAll("button")].find((b) => /Generate install command/.test(b.textContent))?.click());
await page.waitForFunction(() => document.body.innerText.includes("podman run"), { timeout: 10000 });
await sleep(500);
body = await text();
check(
  "the command carries exactly what was chosen",
  /LAZYCAKE_OFFER_CORES=8\b/.test(body) &&
    /LAZYCAKE_OFFER_MEMORY_MB=8192/.test(body) &&
    /LAZYCAKE_OFFER_DISK_MB=102400/.test(body) &&
    /LAZYCAKE_OFFER_NETWORK_MBPS=500/.test(body)
);
check("and uses host networking so the agent can read the link speed", /--network=host/.test(body));
await shot("addmachine-command");

check("no uncaught page errors", errors.length === 0, errors.join(" | "));
await browser.close();
const bad = results.filter((r) => !r.ok);
console.log(`\n${results.length - bad.length}/${results.length} checks passed`);
process.exit(bad.length ? 1 : 0);
