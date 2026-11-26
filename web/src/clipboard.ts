/** Copy on HTTPS and on self-hosted HTTP pages where Clipboard API is absent. */
export async function copyText(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch {
      // Permission can be denied even when the API exists; try the user-gesture fallback.
    }
  }
  const previousFocus = document.activeElement;
  const selection = document.getSelection();
  const ranges = selection
    ? Array.from({ length: selection.rangeCount }, (_, i) =>
        selection.getRangeAt(i).cloneRange(),
      )
    : [];
  // Textarea values normalize CRLF. Write the original string in the copy event
  // so the fallback preserves source text, including whitespace and line endings.
  const onCopy = (event: ClipboardEvent) => {
    if (event.clipboardData) {
      event.clipboardData.setData("text/plain", text);
      event.preventDefault();
    }
  };
  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.readOnly = true;
  textarea.tabIndex = -1;
  textarea.setAttribute("aria-hidden", "true");
  textarea.style.cssText =
    "position:fixed;left:0;top:0;width:1px;height:1px;opacity:0;font-size:16px";
  document.body.append(textarea);
  document.addEventListener("copy", onCopy);
  try {
    textarea.focus({ preventScroll: true });
    textarea.select();
    textarea.setSelectionRange(0, text.length);
    if (!document.execCommand("copy"))
      throw new Error("复制失败，请打开编辑面板手动复制原文。");
  } finally {
    document.removeEventListener("copy", onCopy);
    textarea.remove();
    if (previousFocus instanceof HTMLElement)
      previousFocus.focus({ preventScroll: true });
    if (selection) {
      selection.removeAllRanges();
      ranges.forEach((range) => selection.addRange(range));
    }
  }
}
