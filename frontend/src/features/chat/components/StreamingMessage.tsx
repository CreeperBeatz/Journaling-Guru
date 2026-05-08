import { motion, useReducedMotion } from "motion/react";

import { easeStandard } from "@/lib/motion";

interface Props {
  text: string;
}

// StreamingMessage renders the in-flight assistant turn. The streaming
// itself IS the effect — no synthetic typewriter delay layered on top.
// We just append text as it arrives and run a 120ms opacity fade per
// chunk batch (deliberate: short enough to feel alive, long enough to
// be perceived).
//
// Reduced-motion: drop the per-chunk wrapper and just render the
// running string flat — the appended characters are still visible
// frame-to-frame.
//
// Below the bubble: a three-dot terracotta loader pulses while the
// partial is non-empty. When 'done' lands the parent unmounts this
// component (the persisted assistant row replaces it).
export function StreamingMessage({ text }: Props) {
  const reduced = useReducedMotion();
  return (
    <div className="flex w-full justify-start">
      <div className="min-w-0 max-w-[85%] sm:max-w-[75%]">
        <motion.div
          initial={reduced ? false : { opacity: 0, y: 6 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.18, ease: easeStandard }}
          className="rounded-2xl border border-border/60 bg-card px-4 py-3 text-base leading-relaxed text-card-foreground"
        >
          <p className="whitespace-pre-wrap [overflow-wrap:anywhere]">
            {text}
            <span className="inline-block h-[1.1em] w-[2px] translate-y-[2px] bg-foreground/60 motion-safe:animate-pulse" />
          </p>
        </motion.div>
        <ThreeDotsLoader />
      </div>
    </div>
  );
}

function ThreeDotsLoader() {
  const reduced = useReducedMotion();
  if (reduced) {
    return <p className="mt-2 pl-1 text-xs text-accent/80">thinking…</p>;
  }
  return (
    <div className="mt-2 flex items-center gap-1 pl-1">
      {[0, 1, 2].map((i) => (
        <motion.span
          key={i}
          className="h-1.5 w-1.5 rounded-full bg-accent/70"
          animate={{ opacity: [0.3, 1, 0.3] }}
          transition={{
            duration: 1.1,
            repeat: Infinity,
            delay: i * 0.15,
            ease: "easeInOut",
          }}
        />
      ))}
    </div>
  );
}
