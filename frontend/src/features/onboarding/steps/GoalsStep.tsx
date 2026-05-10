import { motion, useReducedMotion } from "motion/react";
import { Target } from "lucide-react";

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { listContainer, listItem } from "@/lib/motion";

interface Props {
  isFinishing: boolean;
  onFinish: () => void;
  onBack?: () => void;
}

export function GoalsStep({ isFinishing, onFinish, onBack }: Props) {
  const reduce = useReducedMotion();
  return (
    <Card>
      <CardHeader className="space-y-2">
        <p className="text-xs uppercase tracking-wider text-muted-foreground">
          Goals
        </p>
        <CardTitle className="font-serif text-2xl">
          Decide what habit you want to try.
        </CardTitle>
        <CardDescription>
          At your weekly reflection, you can set a goal for the next week.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
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
            <Target className="size-3.5" />
            Examples
          </motion.p>
          <motion.ul
            variants={listItem}
            className="mt-2 space-y-1.5 text-sm text-foreground"
          >
            <li>· Walk 20 minutes after lunch.</li>
            <li>· No phone before breakfast.</li>
            <li>· Message one friend I miss.</li>
          </motion.ul>
        </motion.div>

        <p className="text-sm text-muted-foreground">
          Don't worry about this too much now. On your first weekly reflection, we'll shape it together.
        </p>

        <div className="flex items-center justify-between gap-3">
          {onBack ? (
            <Button variant="ghost" onClick={onBack} disabled={isFinishing}>
              Back
            </Button>
          ) : (
            <span />
          )}
          <Button onClick={onFinish} disabled={isFinishing}>
            {isFinishing ? "Finishing…" : "Start journaling"}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
