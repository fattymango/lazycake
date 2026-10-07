// Browser check for the "Add a machine" page: capacity on one side (whole numbers, a slider with checkpoints, a
// field that can go past the slider, warnings) and the generated install steps beside it.
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
await page.setViewport({ width: 1440, height: 1200 });
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
// Type like a person: select the field's text and type over it.
const typeInto = async (label, v) => {
  const sel = `input[aria-label="${label} to lend"]`;
  await page.focus(sel);
  await page.keyboard.down("Control");
  await page.keyboard.press("KeyA");
  await page.keyboard.up("Control");
  await page.keyboard.type(String(v));
  await sleep(150);
};
const clickStop = (sliderLabel, stop) =>
  page.evaluate(
    (label, stop) => {
      const range = document.querySelector(`input[aria-label="${label}"]`);
      const btn = [...range.parentElement.querySelectorAll("button")].find((b) => b.textContent === String(stop));
      btn?.click();
      return !!btn;
    },
    sliderLabel,
    stop
  );
const stopsOf = (sliderLabel) =>
  page.evaluate(
    (label) =>
      [...document.querySelector(`input[aria-label="${label}"]`).parentElement.querySelectorAll("button")].map((b) =>
        Number(b.textContent.replace(/,/g, ""))
      ),
    sliderLabel
  );
const genButton = () =>
  page.evaluate(() => [...document.querySelectorAll("button")].find((b) => /Generate install command/.test(b.textContent))?.disabled);

// --- layout ------------------------------------------------------------------------------------------
const cards = await page.$$eval("main#main h2, main#main [class*=CardHeader], main#main h3", () => []);
const boxes = await page.evaluate(() => {
  const find = (t) =>
    [...document.querySelectorAll("main#main *")].find((e) => e.children.length === 0 && e.textContent.trim().startsWith(t));
  const r = (e) => e && e.getBoundingClientRect().toJSON();
  return { lend: r(find("1 · What to lend")), install: r(find("2 · Install")) };
});
check(
  "capacity is at the top of the page, on the left",
  boxes.lend && boxes.lend.top < 260 && boxes.lend.left < 500,
  JSON.stringify(boxes.lend && { top: Math.round(boxes.lend.top), left: Math.round(boxes.lend.left) })
);
check(
  "the generated steps are beside it, on the right, at the same height",
  boxes.install && boxes.install.left > boxes.lend.left + 300 && Math.abs(boxes.install.top - boxes.lend.top) < 20
);

let body = await text();
check(
  "CPU, memory, storage and network are all there",
  ["CPU", "Memory", "Storage", "Network"].every((l) => body.includes(l))
);
check(
  "defaults are whole numbers",
  (await val("CPU")) === "2" && (await val("Memory")) === "2" && (await val("Storage")) === "10" && (await val("Network")) === "100"
);
check("each has a slider with checkpoints", (await page.$$('input[type="range"]')).length === 4);
check("CPU checkpoints are 1, 2, 4, 8, 16, 24 (capped at 24)", JSON.stringify(await stopsOf("CPU slider")) === "[1,2,4,8,16,24]");
check(
  "memory tops out at 64 GB and storage at 100 GB on the slider",
  (await stopsOf("Memory slider")).at(-1) === 64 && (await stopsOf("Storage slider")).at(-1) === 100
);

// The warning about not being able to provide it is always there.
check(
  "it warns that an agent that can't provide the numbers shuts down and the machine fails",
  /can't provide these numbers, the agent shuts down/.test(body) && /fails to connect/.test(body)
);
check("no big-capacity warning for a modest offer", !/That's a lot of this machine/.test(body));

// --- checkpoints ---------------------------------------------------------------------------------------
check("clicking a checkpoint sets the value", (await clickStop("CPU slider", 8)) && (await val("CPU")) === "8");
check("and a checkpoint can be 16", (await clickStop("CPU slider", 16)) && (await val("CPU")) === "16");
body = await text();
check(
  "a big offer warns what will be set aside and that the device may not feel smooth",
  /That's a lot of this machine/.test(body) && /16 CPU cores/.test(body) && /smooth experience/.test(body),
  body.match(/The agent will set aside[^\n]*/)?.[0]
);
await shot("addmachine-big");
await clickStop("CPU slider", 2);
check("back to a small value, the warning goes away", !/That's a lot of this machine/.test(await text()));

// --- whole numbers only, and no overflow --------------------------------------------------------------
await typeInto("CPU", "2.5");
check("a decimal point can't be typed: 2.5 becomes 25, never a fraction", (await val("CPU")) === "25", await val("CPU"));
await typeInto("CPU", "-4");
check("a minus sign can't be typed", (await val("CPU")) === "4");
await typeInto("CPU", "1e3");
check("an exponent can't be typed", /^\d+$/.test(await val("CPU")), await val("CPU"));
await typeInto("Memory", "99999999999999999999999999");
const mem = await val("Memory");
check("a huge number is cut to the ceiling, never passed on", Number(mem) <= 16384 && mem.length <= 5, mem);
check(
  "the field itself refuses more digits than the ceiling has",
  (await page.$eval('input[aria-label="Memory to lend"]', (e) => e.maxLength)) === 5
);
await typeInto("Memory", "2");
await typeInto("CPU", "2");

// --- the field can go past the slider ---------------------------------------------------------------
await typeInto("CPU", "48");
body = await text();
check("the field accepts a number past the slider's top (48 cores)", (await val("CPU")) === "48" && !/CPU[^\n]*only has/.test(body));
check("the slider stays at its end", await page.$eval('input[aria-label="CPU slider"]', (e) => Number(e.value) === Number(e.max)));
check("and says it is beyond the slider", /[Bb]eyond the slider/.test(body));
check("the install command can still be generated", (await genButton()) === false);
await typeInto("CPU", "2");

// --- the machine's real limits -------------------------------------------------------------------------
await page.type('textarea[aria-label="Output of the capacity command"]', "cores=12 memory_mb=15314 disk_mb=441802 network_mbps=1000");
await sleep(300);
body = await text();
check(
  "pasting the capacity line is understood, in whole numbers",
  /Got it: 12 cores, 14 GB memory, 431 GB free, 1000 Mbps link/.test(body),
  body.match(/Got it:[^\n]*/)?.[0]
);
await typeInto("CPU", "48");
check(
  "past the slider is fine on a big machine but not on this one: 48 cores is flagged",
  /This machine only has 12 cores/.test(await text())
);
check("and the command can't be generated", (await genButton()) === true);
await typeInto("CPU", "8");
await typeInto("Memory", "16");
check("more memory than the machine has is flagged", /only has 14 GB/.test(await text()));
await typeInto("Memory", "8");
await typeInto("Storage", "500");
check("more storage than is free is flagged", /only has 431 GB free/.test(await text()));
await typeInto("Storage", "100");
await typeInto("Network", "2000");
check("a network offer above the link speed is flagged", /network link is 1,000 Mbps/.test(await text()));
await shot("addmachine-errors");
await typeInto("Network", "500");
check("with everything inside the limits, generating is allowed", (await genButton()) === false);
check("with the machine known, half of it or more still warns (8 of 12 cores)", /That's a lot of this machine/.test(await text()));

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
await typeInto("CPU", "4");
check("changing a number afterwards marks the command out of date", /You changed the numbers/.test(await text()));
await shot("addmachine-command");

check("no uncaught page errors", errors.length === 0, errors.join(" | "));
await browser.close();
const bad = results.filter((r) => !r.ok);
console.log(`\n${results.length - bad.length}/${results.length} checks passed`);
process.exit(bad.length ? 1 : 0);
