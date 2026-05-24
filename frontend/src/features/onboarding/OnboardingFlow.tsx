import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { useNavigate } from "react-router-dom";
import { ArrowRight } from "lucide-react";

import { ApiError } from "@/api/client";
import { Button } from "@/components/ui/button";
import { toast } from "@/components/ui/sonner";
import { ME_KEY } from "@/features/auth/useAuth";
import type { User } from "@/features/auth/api";
import { updateMe, type UpdateMePatch } from "@/features/settings/api";
import { hhmmToMinutes, minutesToHHMM } from "@/lib/dayStart";
import { easeStandard, easeExit } from "@/lib/motion";
import { cn } from "@/lib/utils";

import { WelcomeStep } from "./steps/WelcomeStep";
import { NameStep } from "./steps/NameStep";
import { InputModesStep } from "./steps/InputModesStep";
import { ReminderStep } from "./steps/ReminderStep";
import { DayStartStep } from "./steps/DayStartStep";
import { WeeklyStep } from "./steps/WeeklyStep";

export interface OnboardingDraft {
  displayName: string;
  reminderTime: string; // "HH:MM"
  reminderEnabled: boolean;
  // dayStart is the late-night cutoff (HH:MM). 06:00 default — anything
  // written before this folds into the previous day's entry.
  dayStart: string;
  reflectionWeekday: number; // 0=Sun..6=Sat
}

const STEP_KEYS = [
  "welcome",
  "modes",
  "name",
  "reminder",
  "dayStart",
  "weekly",
] as const;
type StepKey = (typeof STEP_KEYS)[number];

interface Props {
  user: User;
  replay: boolean;
}

export function OnboardingFlow({ user, replay }: Props) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const reduce = useReducedMotion();

  const [stepIdx, setStepIdx] = useState(0);
  const stepKey: StepKey = STEP_KEYS[stepIdx];

  // Seed the draft from /api/me. On replay, the stored values aren't
  // defaults-anymore-but-real-prefs, so we still want them as starting
  // points — the user can re-confirm or change.
  const [draft, setDraft] = useState<OnboardingDraft>(() => ({
    displayName: user.display_name ?? "",
    reminderTime: user.reminder_time?.slice(0, 5) || "20:00",
    reminderEnabled: user.reminder_enabled,
    dayStart: minutesToHHMM(user.day_start_minutes ?? 360),
    reflectionWeekday: user.reflection_weekday ?? 0,
  }));

  const patchMe = useMutation<User, ApiError, UpdateMePatch>({
    mutationFn: updateMe,
    onSuccess: (u) => qc.setQueryData(ME_KEY, u),
    onError: (err) => toast.error("Couldn't save", { description: err.message }),
  });

  const goNext = () => setStepIdx((i) => Math.min(i + 1, STEP_KEYS.length - 1));
  const goBack = () => setStepIdx((i) => Math.max(i - 1, 0));

  const skip = async () => {
    // Persist whatever they may have already touched, then mark onboarded.
    // Skip from step 1 won't have a draft diff so this is a no-op patch.
    const patch: UpdateMePatch = { mark_onboarded: true };
    const trimmedName = draft.displayName.trim();
    if (trimmedName !== (user.display_name ?? "")) {
      patch.display_name = trimmedName;
    }
    if (draft.reminderTime !== user.reminder_time?.slice(0, 5)) {
      patch.reminder_time = draft.reminderTime;
    }
    if (draft.reminderEnabled !== user.reminder_enabled) {
      patch.reminder_enabled = draft.reminderEnabled;
    }
    const draftDayStart = hhmmToMinutes(draft.dayStart);
    if (draftDayStart !== null && draftDayStart !== (user.day_start_minutes ?? 360)) {
      patch.day_start_minutes = draftDayStart;
    }
    if (draft.reflectionWeekday !== (user.reflection_weekday ?? 0)) {
      patch.reflection_weekday = draft.reflectionWeekday;
    }
    try {
      await patchMe.mutateAsync(patch);
    } catch {
      /* fall through */
    }
    navigate("/", { replace: true });
  };

  const variants = useMemo(
    () => ({
      initial: reduce ? { opacity: 0 } : { opacity: 0, y: 12 },
      animate: {
        opacity: 1,
        y: 0,
        transition: { duration: 0.32, ease: easeStandard },
      },
      exit: reduce
        ? { opacity: 0, transition: { duration: 0.16, ease: easeExit } }
        : {
            opacity: 0,
            y: -8,
            transition: { duration: 0.18, ease: easeExit },
          },
    }),
    [reduce],
  );

  return (
    <div className="space-y-6">
      <ProgressHeader
        stepIdx={stepIdx}
        total={STEP_KEYS.length}
        onSkip={skip}
        skipping={patchMe.isPending}
        replay={replay}
      />

      <div className="relative">
        <AnimatePresence mode="wait" initial={false}>
          <motion.div
            key={stepKey}
            variants={variants}
            initial="initial"
            animate="animate"
            exit="exit"
          >
            <StepBody
              stepKey={stepKey}
              draft={draft}
              setDraft={setDraft}
              onSubmitName={async (displayName) => {
                setDraft((d) => ({ ...d, displayName }));
                if (displayName !== (user.display_name ?? "")) {
                  await patchMe.mutateAsync({ display_name: displayName });
                }
                goNext();
              }}
              onSubmitReminder={async (next) => {
                setDraft((d) => ({
                  ...d,
                  reminderTime: next.reminderTime,
                  reminderEnabled: next.reminderEnabled,
                }));
                await patchMe.mutateAsync({
                  reminder_time: next.reminderTime,
                  reminder_enabled: next.reminderEnabled,
                });
                goNext();
              }}
              onSubmitDayStart={async (dayStart) => {
                setDraft((d) => ({ ...d, dayStart }));
                const dsm = hhmmToMinutes(dayStart);
                if (dsm !== null) {
                  await patchMe.mutateAsync({ day_start_minutes: dsm });
                }
                goNext();
              }}
              onSubmitWeekly={async (weekday) => {
                setDraft((d) => ({ ...d, reflectionWeekday: weekday }));
                try {
                  await patchMe.mutateAsync({
                    reflection_weekday: weekday,
                    mark_onboarded: true,
                  });
                } catch {
                  /* toast surfaced; still land them on /today */
                }
                navigate("/", { replace: true });
              }}
              onContinue={goNext}
              onBack={stepIdx > 0 ? goBack : undefined}
            />
          </motion.div>
        </AnimatePresence>
      </div>
    </div>
  );
}

interface ProgressHeaderProps {
  stepIdx: number;
  total: number;
  onSkip: () => void;
  skipping: boolean;
  replay: boolean;
}

function ProgressHeader({
  stepIdx,
  total,
  onSkip,
  skipping,
  replay,
}: ProgressHeaderProps) {
  const pct = ((stepIdx + 1) / total) * 100;
  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between text-xs">
        <span className="uppercase tracking-wider text-muted-foreground">
          {replay ? "Walkthrough" : "Setup"} · Step {stepIdx + 1} of {total}
        </span>
        <button
          type="button"
          onClick={onSkip}
          disabled={skipping}
          className={cn(
            "rounded text-muted-foreground transition-colors",
            "hover:text-foreground focus-visible:outline-none focus-visible:ring-2",
            "focus-visible:ring-ring focus-visible:ring-offset-2 ring-offset-background",
            "disabled:opacity-50",
          )}
        >
          {skipping ? "Saving…" : replay ? "Close" : "Skip for now"}
        </button>
      </div>
      <div
        className="h-1 w-full overflow-hidden rounded-full bg-muted"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={total}
        aria-valuenow={stepIdx + 1}
      >
        <motion.div
          className="h-full bg-primary"
          initial={false}
          animate={{ width: `${pct}%` }}
          transition={{ duration: 0.32, ease: easeStandard }}
        />
      </div>
    </div>
  );
}

interface StepBodyProps {
  stepKey: StepKey;
  draft: OnboardingDraft;
  setDraft: React.Dispatch<React.SetStateAction<OnboardingDraft>>;
  onSubmitName: (displayName: string) => Promise<void>;
  onSubmitReminder: (next: {
    reminderTime: string;
    reminderEnabled: boolean;
  }) => Promise<void>;
  onSubmitDayStart: (dayStart: string) => Promise<void>;
  onSubmitWeekly: (weekday: number) => Promise<void>;
  onContinue: () => void;
  onBack?: () => void;
}

function StepBody({
  stepKey,
  draft,
  setDraft,
  onSubmitName,
  onSubmitReminder,
  onSubmitDayStart,
  onSubmitWeekly,
  onContinue,
  onBack,
}: StepBodyProps) {
  switch (stepKey) {
    case "welcome":
      return <WelcomeStep onContinue={onContinue} />;
    case "name":
      return (
        <NameStep
          draft={draft}
          setDraft={setDraft}
          onSubmit={onSubmitName}
          onBack={onBack}
        />
      );
    case "modes":
      return <InputModesStep onContinue={onContinue} onBack={onBack} />;
    case "reminder":
      return (
        <ReminderStep
          draft={draft}
          setDraft={setDraft}
          onSubmit={onSubmitReminder}
          onBack={onBack}
        />
      );
    case "dayStart":
      return (
        <DayStartStep
          draft={draft}
          setDraft={setDraft}
          onSubmit={onSubmitDayStart}
          onBack={onBack}
        />
      );
    case "weekly":
      return (
        <WeeklyStep
          draft={draft}
          setDraft={setDraft}
          onSubmit={onSubmitWeekly}
          onBack={onBack}
        />
      );
    default:
      return null;
  }
}

// FooterNav is a small primitive shared by the read-only steps so they
// pick up consistent Back / Continue affordances without each step
// re-implementing them.
export function FooterNav({
  onContinue,
  onBack,
  continueLabel = "Continue",
  continueDisabled,
  continuePending,
}: {
  onContinue: () => void;
  onBack?: () => void;
  continueLabel?: string;
  continueDisabled?: boolean;
  continuePending?: boolean;
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      {onBack ? (
        <Button variant="ghost" onClick={onBack} disabled={continuePending}>
          Back
        </Button>
      ) : (
        <span />
      )}
      <Button
        onClick={onContinue}
        disabled={continueDisabled || continuePending}
      >
        {continuePending ? "Saving…" : continueLabel}
        {!continuePending ? <ArrowRight className="ml-1 size-4" /> : null}
      </Button>
    </div>
  );
}
