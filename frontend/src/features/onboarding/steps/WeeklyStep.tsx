import { useState } from "react";
import { motion, useReducedMotion } from "motion/react";
import { CalendarDays } from "lucide-react";

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { listContainer, listItem } from "@/lib/motion";
import { WEEKDAY_LABELS } from "@/lib/weekdays";

import type { OnboardingDraft } from "../OnboardingFlow";

interface Props {
  draft: OnboardingDraft;
  setDraft: React.Dispatch<React.SetStateAction<OnboardingDraft>>;
  onSubmit: (weekday: number) => Promise<void>;
  onBack?: () => void;
}

export function WeeklyStep({ draft, setDraft, onSubmit, onBack }: Props) {
  const reduce = useReducedMotion();
  const [pending, setPending] = useState(false);

  const handleContinue = async () => {
    setPending(true);
    try {
      await onSubmit(draft.reflectionWeekday);
    } finally {
      setPending(false);
    }
  };

  return (
    <Card>
      <CardHeader className="space-y-2">
        <p className="text-xs uppercase tracking-wider text-muted-foreground">
          Weekly reflection
        </p>
        <CardTitle className="font-serif text-2xl">
          Once a week, the patterns become visible.
        </CardTitle>
        <CardDescription>
          Pick the day your week settles. Sundays are the default; many
          people prefer Friday.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-2 text-sm">
          <span className="text-foreground">I will reflect on the past week on</span>
          <Select
            value={String(draft.reflectionWeekday)}
            onValueChange={(v) =>
              setDraft((d) => ({ ...d, reflectionWeekday: Number(v) }))
            }
          >
            <SelectTrigger
              id="onb-weekday"
              aria-label="Reflection day"
              className="w-auto min-w-[8rem]"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {WEEKDAY_LABELS.map((label, idx) => (
                <SelectItem key={idx} value={String(idx)}>
                  {label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <span className="text-foreground">.</span>
        </div>

        <motion.div
          className="rounded-md border border-border/60 bg-muted/30 p-4"
          variants={listContainer}
          initial={reduce ? false : "initial"}
          animate="animate"
        >
          <motion.p
            variants={listItem}
            className="flex items-center gap-2 text-xs uppercase tracking-wider text-muted-foreground"
          >
            <CalendarDays className="size-3.5" />
            On {WEEKDAY_LABELS[draft.reflectionWeekday]}s you will see
          </motion.p>
          <motion.ul
            variants={listItem}
            className="mt-2 space-y-1.5 text-sm leading-relaxed text-foreground"
          >
            <li>
              · Insights about what consistently drained your energy, and
              what gave you energy.
            </li>
            <li>· How your mood shifted throughout the week.</li>
            <li>· The ability to set a goal for the new week.</li>
          </motion.ul>
        </motion.div>

        <div className="flex items-center justify-between gap-3">
          {onBack ? (
            <Button variant="ghost" onClick={onBack} disabled={pending}>
              Back
            </Button>
          ) : (
            <span />
          )}
          <Button onClick={handleContinue} disabled={pending}>
            {pending ? "Saving…" : "Continue"}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
