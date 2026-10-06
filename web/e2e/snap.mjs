// Quick one-off screenshot of a URL path: node e2e/snap.mjs /_kit out.png [width] [theme] [--login alice]
import puppeteer from "puppeteer-core";
const [path, out, width = "1440", theme = "dark"] = process.argv.slice(2);
const login = process.argv.includes("--login") ? process.argv[process.argv.indexOf("--login") + 1] : null;
const BASE = process.env.BASE || "http://127.0.0.1:5173";
const browser = await puppeteer.launch({ executablePath: "/usr/bin/google-chrome", headless: "new", args: ["--no-sandbox", "--disable-gpu", "--hide-scrollbars"] });
const page = await browser.newPage();
const errors = [];
page.on("console", (m) => m.type() === "error" && errors.push(m.text().slice(0, 200)));
page.on("pageerror", (e) => errors.push("pageerror: " + String(e).slice(0, 300)));
await page.setViewport({ width: Number(width), height: 900 });
await page.emulateMediaFeatures([{ name: "prefers-color-scheme", value: theme }]);
await page.evaluateOnNewDocument((t) => localStorage.setItem("lc-theme", t), theme);
if (login) {
  await page.goto(BASE + "/login", { waitUntil: "networkidle0" });
  await page.type('input[name="username"]', login);
  await page.type('input[name="password"]', "password123");
  await page.keyboard.press("Enter");
  await page.waitForFunction(() => location.pathname !== "/login", { timeout: 15000 });
}
await page.goto(BASE + path, { waitUntil: "networkidle2", timeout: 20000 }).catch(() => {});
await new Promise((r) => setTimeout(r, 500));
await page.screenshot({ path: out, fullPage: true });
if (errors.length) console.log("console errors:\n" + [...new Set(errors)].join("\n"));
await browser.close();
