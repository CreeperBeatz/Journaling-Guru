import { useState } from "react";
import { Sparkles, X } from "lucide-react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";

import { easeExit, easeStandard } from "@/lib/motion";

const STORAGE_KEY = "journai.chatHintDismissed";

export function ChatFirstTimeHint() {
  const reduced = useReducedMotion();
  const [visible, setVisible] = useState(() => {
    try {
      return localStorage.getItem(STORAGE_KEY) !== "1";
    } catch {
      return true;
    }
  });

  const dismiss = () => {
    try {
      localStorage.setItem(STORAGE_KEY, "1");
    } catch {
      /* private mode / quota — drop persistence, still hide this session */
    }
    setVisible(false);
  };

  return (
    <AnimatePresence initial={false}>
      {visible ? (
        <motion.div
          key="chat-first-time-hint"
          initial={reduced ? false : { opacity: 0, y: 4 }}
          animate={{ opacity: 1, y: 0 }}
          exit={{ opacity: 0, transition: { duration: 0.18, ease: easeExit } }}
          transition={{ duration: 0.22, ease: easeStandard }}
          className="relative mb-4 rounded-lg border border-primary/25 bg-primary/5 px-4 py-3 text-sm"
        >
          <button
            type="button"
            onClick={dismiss}
            aria-label="Dismiss hint"
            className="absolute right-2 top-2 inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-primary/10 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ring-offset-background"
          >
            <X className="size-4" aria-hidden />
          </button>
          <div className="flex items-start gap-2 pr-8">
            <Sparkles
              className="mt-0.5 size-4 shrink-0 text-primary"
              aria-hidden
            />
            <div className="space-y-1.5">
              <p className="font-medium text-foreground">A quick heads-up</p>
              <ul className="space-y-1.5 leading-relaxed text-foreground/80">
                <li>
                  No need to fill the manual mode separately. Say{" "}
                  <span className="font-medium text-foreground">
                    "let's wrap up"
                  </span>{" "}
                  or tap{" "}
                  <span className="font-medium text-foreground">
                    "Wrap up"
                  </span>{" "}
                  when you want to finish your reflection for the day.
                </li>
                <li>
                  I'll adapt to your day. We can sweep
                  through the questions, or reflect on for hours - 
                  just say how you want to proceed.
                </li>
              </ul>
            </div>
          </div>
        </motion.div>
      ) : null}
    </AnimatePresence>
  );
}
