// Copying text where the Clipboard API may be missing: on plain http over the network the browser
// hides it, so the legacy command on a selected field is the fallback.

/** Copies `text`; true when it worked. Without the Clipboard API the text in `field` is selected and
 *  copied with the legacy command (a hidden field holds it when none is given); a false result
 *  means the user must copy it by hand, and `field` is left selected for that. */
export async function copyText(text: string, field?: HTMLInputElement | HTMLTextAreaElement | null): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // Fall through to the legacy command.
  }
  const previous = document.activeElement;
  let temp: HTMLTextAreaElement | null = null;
  let target = field ?? null;
  if (!target) {
    temp = document.createElement('textarea');
    temp.value = text;
    temp.readOnly = true;
    temp.setAttribute('aria-hidden', 'true');
    temp.style.cssText = 'position:fixed;top:0;left:0;opacity:0;pointer-events:none';
    document.body.appendChild(temp);
    target = temp;
  }
  target.focus();
  target.select();
  let ok = false;
  try {
    ok = document.execCommand('copy');
  } catch {
    ok = false;
  }
  temp?.remove();
  if (temp && previous instanceof HTMLElement) previous.focus();
  return ok;
}
