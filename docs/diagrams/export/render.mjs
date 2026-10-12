// Renders an .excalidraw file to SVG and a 2x PNG with Excalidraw's own library in headless Chrome:
//   node render.mjs ../lazycake-task-flow.excalidraw ../lazycake-task-flow      (writes ../lazycake-task-flow.svg and .png)
import puppeteer from "puppeteer-core";
import fs from "fs";
import { fileURLToPath } from "url";

const [src, outBase] = process.argv.slice(2);
const scene = JSON.parse(fs.readFileSync(src, "utf8"));
const bundle = fileURLToPath(new URL("./bundle.js", import.meta.url));
const b = await puppeteer.launch({ executablePath: process.env.CHROME || "/usr/bin/google-chrome", headless: "new", args: ["--no-sandbox", "--disable-gpu"] });
const p = await b.newPage();
p.on("pageerror", (e) => console.log("page error:", String(e).slice(0, 200)));
await p.setContent("<html><body style='margin:0'></body></html>");
await p.addScriptTag({ path: bundle });
const svg = await p.evaluate(async (s) => await window.renderSvg(s), scene);
fs.writeFileSync(outBase + ".svg", svg);
const m = svg.match(/viewBox="0 0 ([\d.]+) ([\d.]+)"/);
const w = Math.ceil(parseFloat(m[1])), h = Math.ceil(parseFloat(m[2]));
const q = await b.newPage();
await q.setViewport({ width: w, height: h, deviceScaleFactor: 2 });
await q.setContent(`<html><body style="margin:0;background:#fff">${svg}</body></html>`);
await new Promise((r) => setTimeout(r, 1500)); // let the embedded fonts settle
await q.screenshot({ path: outBase + ".png", clip: { x: 0, y: 0, width: w, height: h } });
console.log("exported", outBase, `${w}x${h} (png at 2x)`);
await b.close();
