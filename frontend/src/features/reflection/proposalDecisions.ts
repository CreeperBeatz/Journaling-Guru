import type { ChatMessage } from "@/features/chat/api";

// ProposalDecision is the persisted outcome of one inline propose_*
// card, derived from the chat transcript. Backed by the system_event
// row the FE wrote at accept/decline time (see postSystemEvent meta).
// "open" means no matching event yet — card renders the editable form.
export type ProposalDecision =
  | { state: "accepted"; goalId?: string; goalTitle?: string; weeks?: string; outcome?: string }
  | { state: "declined"; goalId?: string; goalTitle?: string }
  | { state: "open" };

// Family ties a propose_* tool call to its accept/decline event pair.
type Family = "goal" | "extend" | "complete" | "intention";

function toolNameToFamily(name: string): Family | null {
  switch (name) {
    case "propose_goal":
      return "goal";
    case "propose_extend_goal":
      return "extend";
    case "propose_complete_goal":
      return "complete";
    case "propose_intention":
      return "intention";
    default:
      return null;
  }
}

interface EventInfo {
  family: Family;
  kind: "accepted" | "declined";
}

function systemEventInfo(content: string): EventInfo | null {
  switch (content) {
    case "user_accepted_goal":
      return { family: "goal", kind: "accepted" };
    case "user_declined_goal":
      return { family: "goal", kind: "declined" };
    case "user_accepted_extend_goal":
      return { family: "extend", kind: "accepted" };
    case "user_declined_extend_goal":
      return { family: "extend", kind: "declined" };
    case "user_accepted_complete_goal":
      return { family: "complete", kind: "accepted" };
    case "user_declined_complete_goal":
      return { family: "complete", kind: "declined" };
    // user_edited_intention is an accept with edited text — the card
    // still flips to its saved state.
    case "user_accepted_intention":
    case "user_edited_intention":
      return { family: "intention", kind: "accepted" };
    case "user_declined_intention":
      return { family: "intention", kind: "declined" };
    default:
      return null;
  }
}

function pickString(meta: Record<string, unknown>, key: string): string | undefined {
  const v = meta[key];
  return typeof v === "string" && v !== "" ? v : undefined;
}

// resolveProposalDecisions walks the transcript in seq order and pairs
// each propose_* assistant message with the system_event that records
// the user's decision. Matching strategy per family:
//   1. By goal_id when both sides have one (extend/complete always do).
//   2. By goal_title when both sides have one (propose_goal does after
//      the recent meta change).
//   3. FIFO fallback when neither matches — handles legacy rows that
//      pre-date the meta payload.
//
// Returns a Map<message_id, ProposalDecision> keyed on the propose_*
// row's id. Cards look themselves up to decide whether to render the
// editable form, the saved card, or the declined card.
export function resolveProposalDecisions(
  messages: ChatMessage[],
): Map<string, ProposalDecision> {
  const result = new Map<string, ProposalDecision>();
  const openByFamily: Record<Family, { msgId: string; args: Record<string, unknown> }[]> = {
    goal: [],
    extend: [],
    complete: [],
    intention: [],
  };

  for (const m of messages) {
    if (m.role === "assistant" && m.tool_name) {
      const family = toolNameToFamily(m.tool_name);
      if (!family) continue;
      openByFamily[family].push({ msgId: m.id, args: (m.tool_args ?? {}) as Record<string, unknown> });
      result.set(m.id, { state: "open" });
      continue;
    }
    if (m.role !== "system_event") continue;
    const evt = systemEventInfo(m.content);
    if (!evt) continue;
    const meta = (m.tool_args ?? {}) as Record<string, unknown>;
    const queue = openByFamily[evt.family];
    if (queue.length === 0) continue;

    const metaGoalId = pickString(meta, "goal_id");
    // Intentions carry their text in intention_text and have no goal_id;
    // reuse the goalTitle slot so cards share one decision shape.
    const metaGoalTitle =
      evt.family === "intention"
        ? pickString(meta, "intention_text")
        : pickString(meta, "goal_title");

    let idx = -1;
    if (metaGoalId) {
      idx = queue.findIndex((p) => p.args.goal_id === metaGoalId);
    }
    if (idx === -1 && metaGoalTitle) {
      idx = queue.findIndex((p) =>
        evt.family === "intention"
          ? p.args.intention === metaGoalTitle
          : p.args.title === metaGoalTitle,
      );
    }
    if (idx === -1) idx = 0; // FIFO fallback for legacy rows
    const open = queue[idx];
    queue.splice(idx, 1);

    if (evt.kind === "accepted") {
      result.set(open.msgId, {
        state: "accepted",
        goalId: metaGoalId,
        goalTitle: metaGoalTitle,
        weeks: pickString(meta, "weeks"),
        outcome: pickString(meta, "outcome"),
      });
    } else {
      result.set(open.msgId, {
        state: "declined",
        goalId: metaGoalId,
        goalTitle: metaGoalTitle,
      });
    }
  }

  return result;
}
