import { motion, useReducedMotion } from "motion/react";
import { MessageSquare, Mic, Pencil } from "lucide-react";
import type { LucideIcon } from "lucide-react";

import {
  Card,
  CardContent,
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
      </CardHeader>
      <CardContent className="space-y-6">
        <motion.dl
          className="grid grid-cols-[auto_auto_1fr] items-baseline gap-x-3 gap-y-4"
          variants={listContainer}
          initial={reduce ? false : "initial"}
          animate="animate"
        >
          {MODES.map(({ icon: Icon, label, hint }) => (
            <motion.div
              key={label}
              variants={listItem}
              className="contents"
            >
              <Icon
                className="size-5 self-center text-muted-foreground"
                aria-hidden
              />
              <dt className="text-center text-sm font-semibold text-foreground">
                {label}
              </dt>
              <dd className="text-xs leading-relaxed text-muted-foreground/70">
                {hint}
              </dd>
            </motion.div>
          ))}
        </motion.dl>

        <FooterNav onContinue={onContinue} onBack={onBack} />
      </CardContent>
    </Card>
  );
}
