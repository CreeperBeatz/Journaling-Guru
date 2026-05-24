import type { ReactNode } from "react";

import type { ChatMessage } from "../api";
import { MessageBubble } from "./MessageBubble";
import { StreamingMessage } from "./StreamingMessage";

interface Props {
  messages: ChatMessage[];
  partial: string;
  /** Optional renderer for inline cards attached to assistant turns
   * that emitted a tool call (e.g. propose_goal). Returns null when no
   * card applies to that message. Daily chat doesn't pass this. */
  renderToolCard?: (message: ChatMessage) => ReactNode;
}

// MessageList renders the persisted bubbles plus the in-flight streaming
// bubble. The streaming bubble shows whenever `partial` has content —
// not gated on a streaming flag — so that the bubble stays mounted from
// the first token through the moment the persisted turn lands in the
// messages array. ChatPanel clears `partial` once it sees that arrival
// (see useEffect on visibleMsgs.length), avoiding the disappear/
// reappear flicker.
//
// Scroll-to-bottom is owned by the parent (ChatPanel) so the auto-follow
// logic can react to both `messages.length` AND `partial.length` while
// respecting the user's manual scroll position.
export function MessageList({ messages, partial, renderToolCard }: Props) {
  return (
    <div className="flex flex-col gap-4">
      {messages.map((m) => {
        const card = renderToolCard ? renderToolCard(m) : null;
        const hasBubble = m.content.trim() !== "";
        return (
          <div key={m.id} className="flex flex-col gap-3">
            {hasBubble ? <MessageBubble message={m} /> : null}
            {card}
          </div>
        );
      })}
      {partial.length > 0 && <StreamingMessage text={partial} />}
    </div>
  );
}
