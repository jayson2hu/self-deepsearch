/** A native modal keeps password entry masked and the rest of the page inert. */
export function requestPassword(signal: AbortSignal, previous: HTMLElement | null): Promise<string | null> {
  if (signal.aborted) return Promise.reject(signal.reason);

  return new Promise((resolve, reject) => {
    const dialog = document.createElement("dialog");
    dialog.className = "password-confirmation";
    dialog.setAttribute("aria-labelledby", "password-confirmation-title");
    dialog.setAttribute("aria-describedby", "password-confirmation-description");

    const form = document.createElement("form");
    const title = document.createElement("h2");
    title.id = "password-confirmation-title";
    title.textContent = "确认当前账号密码";
    const description = document.createElement("p");
    description.id = "password-confirmation-description";
    description.textContent = "该操作需要近期密码确认。取消后不会继续执行。";
    const label = document.createElement("label");
    label.htmlFor = "password-confirmation-input";
    label.textContent = "当前账号密码";
    const input = document.createElement("input");
    input.id = "password-confirmation-input";
    input.name = "password";
    input.type = "password";
    input.autocomplete = "current-password";
    input.required = true;
    input.maxLength = 128;
    input.autofocus = true;
    const actions = document.createElement("div");
    actions.className = "password-confirmation__actions";
    const cancel = document.createElement("button");
    cancel.type = "button";
    cancel.textContent = "取消";
    const submit = document.createElement("button");
    submit.type = "submit";
    submit.className = "primary";
    submit.textContent = "确认密码并继续";
    actions.append(cancel, submit);
    form.append(title, description, label, input, actions);
    dialog.append(form);

    let finished = false;
    const finish = (password: string | null, aborted = false) => {
      if (finished) return;
      finished = true;
      input.value = "";
      signal.removeEventListener("abort", abort);
      if (dialog.open) dialog.close();
      dialog.remove();
      restoreFocus(previous);
      if (aborted) reject(signal.reason);
      else resolve(password);
    };
    const abort = () => finish(null, true);
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      if (form.reportValidity()) finish(input.value);
    });
    cancel.addEventListener("click", () => finish(null));
    dialog.addEventListener("cancel", (event) => {
      event.preventDefault();
      finish(null);
    });
    dialog.addEventListener("close", () => finish(null));
    dialog.addEventListener("keydown", (event) => {
      if (event.key !== "Tab") return;
      if (event.shiftKey && document.activeElement === input) {
        event.preventDefault();
        submit.focus();
      } else if (!event.shiftKey && document.activeElement === submit) {
        event.preventDefault();
        input.focus();
      }
    });
    signal.addEventListener("abort", abort, { once: true });
    document.body.append(dialog);
    try {
      dialog.showModal();
      input.focus();
    } catch (error) {
      input.value = "";
      signal.removeEventListener("abort", abort);
      dialog.remove();
      reject(error);
    }
  });
}

function restoreFocus(previous: HTMLElement | null) {
  if (!previous?.isConnected) return;
  // The initiating action often stays disabled until adminFetch settles. Wait
  // for that state change without stealing focus if the user moves elsewhere.
  const observer = new MutationObserver(restore);
  function restore() {
    if (!previous?.isConnected || (document.activeElement !== document.body && document.activeElement !== previous)) {
      observer.disconnect();
      clearTimeout(timer);
    } else if (!previous.matches(":disabled")) {
      previous.focus({ preventScroll: true });
      observer.disconnect();
      clearTimeout(timer);
    }
  }
  observer.observe(previous, { attributes: true, attributeFilter: ["disabled"] });
  const timer = setTimeout(() => observer.disconnect(), 15000);
  queueMicrotask(restore);
}
