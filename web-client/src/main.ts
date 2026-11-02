import { createControlPlane, createTokenHolder } from "./api.js";
import { createLifecycle } from "./lifecycle.js";
import { render, type Targets } from "./ui.js";

function requireElement(id: string): HTMLElement {
  const element = document.getElementById(id);
  if (element === null) throw new Error(`web-client: missing element ${id}`);
  return element;
}

const tokenField = requireElement("token") as HTMLInputElement;
const regionField = requireElement("region") as HTMLInputElement;
const statusText = requireElement("status");
const sessionText = requireElement("session");
const remainingText = requireElement("remaining");
const openLink = requireElement("open") as HTMLAnchorElement;
const startButton = requireElement("start") as HTMLButtonElement;
const stopButton = requireElement("stop") as HTMLButtonElement;

const holder = createTokenHolder();
const plane = createControlPlane(fetch, holder.value);

const targets: Targets = {
  status: { setText: (value) => (statusText.textContent = value) },
  session: { setText: (value) => (sessionText.textContent = value) },
  remaining: { setText: (value) => (remainingText.textContent = value) },
  start: { setEnabled: (value) => (startButton.disabled = !value) },
  stop: { setEnabled: (value) => (stopButton.disabled = !value) },
  open: {
    setHref: (value) => {
      if (value === null) {
        openLink.removeAttribute("href");
        return;
      }
      openLink.href = value;
    },
    setVisible: (value) => (openLink.hidden = !value),
  },
};

const lifecycle = createLifecycle({
  plane,
  now: () => Date.now(),
  wait: (ms) => new Promise<void>((resolve) => window.setTimeout(resolve, ms)),
  onUpdate: (snapshot) => render(snapshot, targets, Date.now()),
});

startButton.addEventListener("click", () => {
  holder.set(tokenField.value);
  tokenField.value = "";
  void lifecycle.start(regionField.value.trim());
});

stopButton.addEventListener("click", () => {
  void lifecycle.stop();
});

openLink.rel = "noreferrer noopener";
openLink.referrerPolicy = "no-referrer";

window.setInterval(() => {
  void lifecycle.expireCheck();
  render(lifecycle.snapshot(), targets, Date.now());
}, 1_000);

window.addEventListener("pagehide", () => {
  lifecycle.unload();
});

render(lifecycle.snapshot(), targets, Date.now());
