import { motion, useReducedMotion } from "motion/react";
import { Battery, BatteryLow, Heart } from "lucide-react";
import type { LucideIcon } from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { listContainer, listItem } from "@/lib/motion";

import { FooterNav } from "../OnboardingFlow";

interface QuestionRow {
  icon: LucideIcon;
  label: string;
}

const QUESTIONS: QuestionRow[] = [
  { icon: Battery, label: "What charged you?" },
  { icon: BatteryLow, label: "What drained you?" },
  { icon: Heart, label: "What are you grateful for?" },
];

export function WelcomeStep({ onContinue }: { onContinue: () => void }) {
  const reduce = useReducedMotion();
  return (
    <Card>
      <CardHeader className="space-y-2">
        <p className="text-xs uppercase tracking-wider text-muted-foreground">
          Welcome
        </p>
        <CardTitle className="font-serif text-3xl leading-tight">
          Say hello to your Journaling Guru.
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-6">
        <motion.ul
          className="space-y-2"
          variants={listContainer}
          initial={reduce ? false : "initial"}
          animate="animate"
        >
          {QUESTIONS.map(({ icon: Icon, label }) => (
            <motion.li
              key={label}
              variants={listItem}
              className="flex items-center gap-3 rounded-md border border-border/60 bg-muted/30 px-3 py-3"
            >
              <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
              <p className="text-sm font-medium text-foreground">{label}</p>
            </motion.li>
          ))}
        </motion.ul>

        <div className="space-y-2 text-sm leading-relaxed text-muted-foreground">
          <p>
            Three questions, three minutes a day. Over weeks, you'll start
            to see patterns.
          </p>
          <p>Let's set you up — takes about a minute.</p>
        </div>

        <FooterNav onContinue={onContinue} continueLabel="Begin" />
      </CardContent>
    </Card>
  );
}
