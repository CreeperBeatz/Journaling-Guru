// beforeinstallprompt fires once, very early, on Android Chrome / Edge /
// Samsung Internet (and desktop Chromium) when the page meets PWA install
// criteria. If we don't capture it before React mounts the route that
// wants to show an install button, it's gone — the event is not replayed.
//
// So we run a module-level listener at app boot, stash the event, and
// expose a tiny pub/sub for React components to subscribe to changes via
// useSyncExternalStore. iOS Safari never fires this event; that path
// keeps the existing manual "Add to Home Screen" copy.

type BIPEvent = Event & {
  readonly platforms: ReadonlyArray<string>;
  prompt: () => Promise<void>;
  userChoice: Promise<{ outcome: "accepted" | "dismissed"; platform: string }>;
};

let deferred: BIPEvent | null = null;
let installed = false;
let initialized = false;

const listeners = new Set<() => void>();

function notify() {
  for (const cb of listeners) cb();
}

export function initInstallCapture(): void {
  if (initialized || typeof window === "undefined") return;
  initialized = true;

  window.addEventListener("beforeinstallprompt", (e) => {
    e.preventDefault();
    deferred = e as BIPEvent;
    notify();
  });

  window.addEventListener("appinstalled", () => {
    deferred = null;
    installed = true;
    notify();
  });
}

export interface InstallState {
  canInstall: boolean;
  installed: boolean;
}

// Cached snapshot — useSyncExternalStore requires a stable reference when
// the underlying state hasn't changed, otherwise React tears.
let snapshot: InstallState = { canInstall: false, installed: false };

function refreshSnapshot() {
  const next: InstallState = {
    canInstall: deferred !== null,
    installed,
  };
  if (next.canInstall !== snapshot.canInstall || next.installed !== snapshot.installed) {
    snapshot = next;
  }
}

export function getInstallState(): InstallState {
  refreshSnapshot();
  return snapshot;
}

export function subscribe(cb: () => void): () => void {
  listeners.add(cb);
  return () => {
    listeners.delete(cb);
  };
}

export type InstallOutcome = "accepted" | "dismissed" | "unavailable";

export async function triggerInstall(): Promise<InstallOutcome> {
  const evt = deferred;
  if (!evt) return "unavailable";
  // The event is single-use — clear it before awaiting so concurrent
  // re-renders don't try to call prompt() twice.
  deferred = null;
  notify();
  try {
    await evt.prompt();
    const choice = await evt.userChoice;
    return choice.outcome;
  } catch {
    return "dismissed";
  }
}
