import { useState } from "react";

import { Card, CardContent } from "@/components/ui/card";

import { type ChatMessage } from "@/features/chat/api";
import { MessageBubble } from "@/features/chat/components/MessageBubble";

import { useWeeklyChatByWeek } from "./hooks";

interface Props {
  weekStart: string;
}

// HistoryWeeklyChatTranscript — read-only transcript for a past week's
// weekly-reflection chat. Mirrors the daily HistoryChatTranscript:
// default-collapsed <details>, expand to read the bubble list. Hidden
// entirely when no chat exists for that week.
//
// Tool-call rows (propose_goal etc.) without text are filtered out;
// the user / assistant text bubbles tell the story of the conversation.
export function HistoryWeeklyChatTranscript({ weekStart }: Props) {
  const [open, setOpen] = useState(false);
  const query = useWeeklyChatByWeek(weekStart);

  if (query.isPending) return null;
  if (query.isError) return null; // 404 = no chat for that week
  const session = query.data?.session;
  const messages: ChatMessage[] = query.data?.messages ?? [];
  if (!session || messages.length === 0) return null;

  // Visible filter: user + assistant rows that carry actual text. Tool-
  // call-only assistant rows (no content) are dropped — they were
  // editable cards live, but have no read-only equivalent here.
  const visible = messages.filter((m) => {
    if (m.role !== "user" && m.role !== "assistant") return false;
    return m.content.trim() !== "";
  });
  if (visible.length === 0) return null;

  const turnCount = visible.filter((m) => m.role === "user").length;

  return (
    <Card>
      <CardContent className="px-5 py-4">
        <details
          open={open}
          onToggle={(e) => setOpen((e.target as HTMLDetailsElement).open)}
        >
          <summary className="cursor-pointer list-none">
            <div className="flex items-center justify-between gap-3">
              <div>
                <p className="font-serif text-base text-foreground">
                  Reflection conversation
                </p>
                <p className="text-xs text-muted-foreground">
                  {turnCount} {turnCount === 1 ? "turn" : "turns"} with the
                  reflection companion
                </p>
              </div>
              <span aria-hidden className="text-xs text-muted-foreground">
                {open ? "Hide" : "Show"}
              </span>
            </div>
          </summary>
          <div className="mt-4 flex flex-col gap-4">
            {visible.map((m) => (
              <MessageBubble key={m.id} message={m} />
            ))}
          </div>
        </details>
      </CardContent>
    </Card>
  );
}
