// Helpers for the line charts (ui/LineChart): read how many points a chart has and hover them.
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

export const chartInfo = (page) => page.$eval("[data-chart]", (e) => ({ points: Number(e.dataset.points), gaps: Number(e.dataset.gaps) }));

/** Move the pointer over point `i` of the first chart on the page and return its tooltip text. */
export async function hoverIndex(page, i) {
  const { points } = await chartInfo(page);
  const overlay = await page.$("[data-chart] svg > rect:last-of-type");
  const box = await overlay.boundingBox();
  const f = points <= 1 ? 0.5 : i / (points - 1);
  await page.mouse.move(box.x + f * box.width, box.y + box.height / 2);
  await sleep(150);
  return page.$eval("[data-chart-tip]", (e) => e.innerText).catch(() => "");
}

/** Hover points from the newest backwards until one's tooltip satisfies `pred`; returns that tooltip ("" if none). */
export async function hoverFind(page, pred) {
  const { points } = await chartInfo(page);
  for (let i = points - 1; i >= 0; i--) {
    const tip = await hoverIndex(page, i);
    if (tip && pred(tip)) return tip;
  }
  return "";
}
