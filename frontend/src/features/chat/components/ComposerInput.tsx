import { useEffect, useRef, useState } from "react";
import { Loader2, Send } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

interface Props {
  onSend: (content: string) => void;
  disabled?: boolean;
  pending?: boolean;
  placeholder?: string;
}

const MAX_LEN = 4_000;

// ComposerInput is the textarea + send button at the bottom of the
// chat. Auto-grows up to ~6 lines, then scrolls. Enter submits;
// Shift+Enter inserts a newline. The send button is the only mouse
// affordance — Cmd/Ctrl+Enter also fires it for keyboard heavies.
export function ComposerInput({ onSend, disabled, pending, placeholder }: Props) {
  const [value, setValue] = useState("");
  const ref = useRef<HTMLTextAreaElement>(null);

  // Auto-grow the textarea by syncing height to scrollHeight on every
  // change. Capped via `max-h-[18ch]` in CSS — past that, internal
  // scroll kicks in.
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 240)}px`;
  }, [value]);

  const submit = () => {
    const trimmed = value.trim();
    if (!trimmed || disabled || pending) return;
    onSend(trimmed);
    setValue("");
    requestAnimationFrame(() => ref.current?.focus());
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      submit();
    }
  };

  return (
    <div
      className={cn(
        "flex items-end gap-2 rounded-xl border border-border/80 bg-background px-3 py-2",
        "focus-within:border-foreground/30 focus-within:ring-1 focus-within:ring-foreground/10",
        disabled && "opacity-60",
      )}
    >
      <textarea
        ref={ref}
        value={value}
        onChange={(e) => {
          if (e.target.value.length <= MAX_LEN) setValue(e.target.value);
        }}
        onKeyDown={onKeyDown}
        rows={1}
        disabled={disabled}
        placeholder={placeholder ?? "Say something…"}
        className={cn(
          "flex-1 resize-none bg-transparent py-2 text-base leading-relaxed",
          "placeholder:text-muted-foreground focus:outline-none",
          "min-h-[2.5rem] max-h-60",
        )}
      />
      <Button
        type="button"
        size="icon"
        variant="default"
        onClick={submit}
        disabled={disabled || pending || value.trim().length === 0}
        aria-label="Send message"
        className="shrink-0"
      >
        {pending ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : (
          <Send className="h-4 w-4" />
        )}
      </Button>
    </div>
  );
}
