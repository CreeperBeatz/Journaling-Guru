import type { ChatPhase } from "../api";

interface Props {
  phase: ChatPhase | undefined;
}

// ChatHeader is now a tight phase-label strip. Finish / Restart moved
// into the composer's kebab menu (see ComposerActions); a Wrap-up
// affordance lives next to the kebab when topics remain uncovered.
// Keeping the phase label visible signals the bot's mode change after
// a wrap-up trigger.
export function ChatHeader({ phase }: Props) {
  const label = phaseLabel(phase);
  if (!label) return null;
  return (
    <p className="min-w-0 text-xs uppercase tracking-wide text-muted-foreground">
      {label}
    </p>
  );
}

function phaseLabel(phase: ChatPhase | undefined): string {
  switch (phase) {
    case "greeting":
      return "Starting up";
    case "exploring":
      return "Reflecting";
    case "wrapping_up":
      return "Wrapping up";
    case "finalized":
      // Reachable only briefly mid-extraction. The extraction runs in
      // the background (status pill in the Today sticky bar). Once the
      // worker completes it rolls phase back to exploring.
      return "Reflecting";
    case "abandoned":
      return "Reflecting";
    default:
      return "";
  }
}
