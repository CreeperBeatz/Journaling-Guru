import { motion, useReducedMotion } from "motion/react";

import { cn } from "@/lib/utils";
import { easeStandard } from "@/lib/motion";
import type { ChatMessage } from "../api";

interface Props {
  message: ChatMessage;
}

// MessageBubble is a single persisted turn. User-side aligns right with
// a soft primary tint; assistant-side aligns left with the warm card
// background — the bubble border is intentionally light so the eye
// reads the conversation as one continuous fabric, not a chat panel.
//
// Layout choice: we don't show timestamps or avatars. This is a
// reflection space, not a messaging app — chrome would break the
// "ink on paper" intent.
export function MessageBubble({ message }: Props) {
  const reduced = useReducedMotion();
  const isUser = message.role === "user";
  return (
    <motion.div
      initial={reduced ? false : { opacity: 0, y: 6 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.18, ease: easeStandard }}
      className={cn(
        "flex w-full",
        isUser ? "justify-end" : "justify-start",
      )}
    >
      <div
        className={cn(
          "max-w-[85%] rounded-2xl px-4 py-3 text-base leading-relaxed sm:max-w-[75%]",
          isUser
            ? "bg-primary/8 text-foreground"
            : "border border-border/60 bg-card text-card-foreground",
        )}
      >
        <p className="whitespace-pre-wrap">{message.content}</p>
      </div>
    </motion.div>
  );
}
