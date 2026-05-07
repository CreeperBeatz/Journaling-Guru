import { useState } from "react";

import { Card, CardContent } from "@/components/ui/card";

import { type ChatMessage } from "./api";
import { useChatByDate, visibleMessages } from "./hooks";
import { MessageBubble } from "./components/MessageBubble";

interface Props {
  localDate: string;
}

// HistoryChatTranscript renders the read-only transcript for a past
// day. Default-collapsed <details> so HistoryView stays uncluttered;
// expand reveals the bubble list. Empty state hides the card entirely
// — most history days won't have a chat session yet.
export function HistoryChatTranscript({ localDate }: Props) {
  const [open, setOpen] = useState(false);
  const query = useChatByDate(localDate);

  if (query.isPending) return null;
  if (query.isError) {
    // 404 = no chat for this day; render nothing.
    return null;
  }
  const session = query.data?.session;
  const messages: ChatMessage[] = query.data?.messages ?? [];
  if (!session || messages.length === 0) return null;

  const visible = visibleMessages(messages);
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
                  Conversation transcript
                </p>
                <p className="text-xs text-muted-foreground">
                  {turnCount} {turnCount === 1 ? "turn" : "turns"} ·{" "}
                  {session.extraction_status === "completed"
                    ? "auto-filled into the check-in"
                    : "draft"}
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
