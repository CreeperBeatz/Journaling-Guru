import { motion, useReducedMotion } from "motion/react";

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { easeStandard } from "@/lib/motion";

interface Props {
  onFinalize: () => void;
  pending: boolean;
}

// WrapUpAffordance surfaces a soft "Update check-in?" nudge when the
// model has called propose_wrap_up. It's optional — the user can
// ignore it and keep typing; the next user turn flips the phase back
// to exploring and the affordance disappears. Clicking the action
// triggers the same overwrite-warning AlertDialog as the header
// "Update check-in" button.
export function WrapUpAffordance({ onFinalize, pending }: Props) {
  const reduced = useReducedMotion();
  return (
    <motion.div
      initial={reduced ? false : { opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.22, ease: easeStandard }}
      className="rounded-lg border border-accent/30 bg-accent/5 px-4 py-3 text-sm text-accent-foreground"
    >
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <p className="text-foreground/85">
          Sounds like a good place to pause. Want to refresh today&apos;s check-in with what we&apos;ve covered?
        </p>
        <AlertDialog>
          <AlertDialogTrigger asChild>
            <Button variant="default" size="sm" disabled={pending} className="self-start sm:self-auto">
              {pending ? "Updating…" : "Update check-in"}
            </Button>
          </AlertDialogTrigger>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Update your check-in from this chat?</AlertDialogTitle>
              <AlertDialogDescription>
                Today&apos;s mood, emotions, notes, and any answers covered in conversation
                will be overwritten with what was discussed here. Slots the chat didn&apos;t
                cover stay as-is. You can keep chatting after — running this again will
                re-extract.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Not yet</AlertDialogCancel>
              <AlertDialogAction onClick={onFinalize}>Update check-in</AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
    </motion.div>
  );
}
