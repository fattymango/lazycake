/**
 * Splits a command line into arguments the way a shell would for the common
 * cases: whitespace separates, and single or double quotes group (so
 * `sh -c "sleep 5; echo done"` is three arguments, not six). There is no
 * escaping or expansion; a task runs the argv exactly as given.
 */
export function splitArgs(input: string): string[] {
  const out: string[] = [];
  let cur = "";
  let quote: '"' | "'" | null = null;
  let inToken = false;
  for (const ch of input) {
    if (quote) {
      if (ch === quote) quote = null;
      else cur += ch;
    } else if (ch === '"' || ch === "'") {
      quote = ch;
      inToken = true;
    } else if (/\s/.test(ch)) {
      if (inToken) {
        out.push(cur);
        cur = "";
        inToken = false;
      }
    } else {
      cur += ch;
      inToken = true;
    }
  }
  if (inToken) out.push(cur);
  return out;
}

/** True if the quotes in `input` are balanced. */
export function hasBalancedQuotes(input: string): boolean {
  let quote: string | null = null;
  for (const ch of input) {
    if (quote) {
      if (ch === quote) quote = null;
    } else if (ch === '"' || ch === "'") quote = ch;
  }
  return quote === null;
}

/** The inverse of splitArgs, for showing a stored argv back in a text box. */
export function joinArgs(args: string[]): string {
  return args.map((a) => (a === "" || /[\s"']/.test(a) ? (a.includes('"') ? `'${a}'` : `"${a}"`) : a)).join(" ");
}
