import { motion, useReducedMotion } from "motion/react";
import { MessageSquare, Mic, Pencil } from "lucide-react";
import type { LucideIcon } from "lucide-react";

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { listContainer, listItem } from "@/lib/motion";

import { FooterNav } from "../OnboardingFlow";

interface ModeRow {
  icon: LucideIcon;
  label: string;
  hint: string;
}

const MODES: ModeRow[] = [
  {
    icon: Pencil,
    label: "Manual",
    hint: "Fill in the answers yourself.",
  },
  {
    icon: MessageSquare,
    label: "Chat",
    hint: "Type to the assistant. It will guide you through.",
  },
  {
    icon: Mic,
    label: "Voice",
    hint: "Or talk - same as the chat, but hands-free.",
  },
];

export function InputModesStep({
  onContinue,
  onBack,
}: {
  onContinue: () => void;
  onBack?: () => void;
}) {
  const reduce = useReducedMotion();
  return (
    <Card>
      <CardHeader className="space-y-2">
        <p className="text-xs uppercase tracking-wider text-muted-foreground">
          Three ways in
        </p>
        <CardTitle className="font-serif text-2xl">
          Type it, chat it, or just talk.
        </CardTitle>
        <CardDescription>
          Whichever feels lower-friction tonight. You can switch any time.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        <motion.div
          className="grid gap-3 sm:grid-cols-3"
          variants={listContainer}
          initial={reduce ? false : "initial"}
          animate="animate"
        >
          {MODES.map(({ icon: Icon, label, hint }) => (
            <motion.div
              key={label}
              variants={listItem}
              className="rounded-md border border-border/60 bg-muted/30 p-4"
            >
              <Icon className="size-5 text-muted-foreground" aria-hidden />
              <p className="mt-3 text-sm font-medium text-foreground">
                {label}
              </p>
              <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                {hint}
              </p>
            </motion.div>
          ))}
        </motion.div>

        <FooterNav onContinue={onContinue} onBack={onBack} />
      </CardContent>
    </Card>
  );
}
