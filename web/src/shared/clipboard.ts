// navigator.clipboard only exists in a secure context (HTTPS, or
// localhost) - Firefox enforces this strictly, so on a plain-HTTP,
// non-localhost origin (this VM's own demo deployment included) it's
// simply undefined and a bare `navigator.clipboard.writeText(...)` throws
// synchronously, silently doing nothing wherever that throw isn't caught.
// Chrome is looser about it in some setups, which is why this can look
// fine in one browser and dead in another. Falls back to the classic
// hidden-textarea + execCommand("copy") trick, which works in insecure
// contexts too (deprecated, but still broadly supported - there's no
// secure-context-free replacement).
export async function copyToClipboard(text: string): Promise<boolean> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // fall through to the legacy path below
    }
  }

  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.focus();
  textarea.select();
  let ok = false;
  try {
    ok = document.execCommand("copy");
  } catch {
    ok = false;
  }
  document.body.removeChild(textarea);
  return ok;
}
