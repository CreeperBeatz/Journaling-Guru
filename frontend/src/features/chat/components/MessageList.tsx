import { useEffect, useRef } from "react";

import type { ChatMessage } from "../api";
import { MessageBubble } from "./MessageBubble";
import { StreamingMessage } from "./StreamingMessage";

interface Props {
  messages: ChatMessage[];
  partial: string;
  streaming: boolean;
}

// MessageList renders the persisted bubbles plus the in-flight streaming
// bubble (when streaming=true and partial has content). Auto-scrolls
// to bottom on new messages or growing partial text.
//
// We pin to bottom unconditionally — chat is a "read down" surface,
// not a "manage scroll position" one. If a user scrolls up to read
// earlier turns we'll fight them; that's an acceptable trade for v1.
export function MessageList({ messages, partial, streaming }: Props) {
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [messages.length, partial.length]);

  return (
    <div className="flex flex-col gap-4">
      {messages.map((m) => (
        <MessageBubble key={m.id} message={m} />
      ))}
      {streaming && partial.length > 0 && <StreamingMessage text={partial} />}
      <div ref={bottomRef} />
    </div>
  );
}
