import { useSyncExternalStore } from "react";

import { isStandalone } from "@/features/push/utils";

import {
  getInstallState,
  subscribe,
  triggerInstall,
  type InstallOutcome,
} from "./install";

// useInstallPrompt is the single read surface for the install affordance.
// canInstall is the only flag a button needs: true exactly when we have
// a live beforeinstallprompt in hand and we're not already running as a
// standalone PWA. iOS short-circuits to false because Safari never
// dispatches the event.
export function useInstallPrompt(): {
  canInstall: boolean;
  installed: boolean;
  install: () => Promise<InstallOutcome>;
} {
  const state = useSyncExternalStore(subscribe, getInstallState, getInstallState);
  const standalone = isStandalone();
  return {
    canInstall: state.canInstall && !state.installed && !standalone,
    installed: state.installed || standalone,
    install: triggerInstall,
  };
}
