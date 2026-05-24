import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";

import { ReflectionResponse } from "../api";
import { usePatchReflection } from "../hooks";
import { PatternCard } from "./PatternCard";

interface Props {
  data: ReflectionResponse;
  onContinue: () => void;
  saving: boolean;
}

// Card 1 — pattern view + the "Did anything surprise you?" textarea.
// Surprise text persists to weekly_reflections.surprise_text on blur
// (fire-and-forget). Continue advances to step 2.
export function PatternAndSurpriseCard({ data, onContinue, saving }: Props) {
  const [text, setText] = useState(data.surprise_text);
  const patch = usePatchReflection();

  // Keep state in sync if the cache was repopulated (e.g. after a
  // background refetch). Cheap because the wizard never has stale data
  // for long.
  useEffect(() => {
    setText(data.surprise_text);
  }, [data.surprise_text]);

  const flush = () => {
    const trimmed = text.trim();
    if (trimmed === data.surprise_text.trim()) return;
    patch.mutate({ surprise_text: trimmed });
  };

  const handleContinue = () => {
    flush();
    onContinue();
  };

  const closingQuestion = data.closing_question?.trim() ?? "";
  const promptTitle = closingQuestion || "Did anything surprise you this week?";

  return (
    <div className="space-y-6">
      <PatternCard data={data} />

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="font-serif text-base">{promptTitle}</CardTitle>
          <p className="text-xs italic text-muted-foreground">
            Free text — optional. Saved to this week's reflection.
          </p>
        </CardHeader>
        <CardContent>
          <Textarea
            value={text}
            onChange={(e) => setText(e.target.value)}
            onBlur={flush}
            rows={3}
            maxLength={4000}
            placeholder="Take your time — there's no wrong answer."
            className="border-transparent bg-transparent px-0 leading-prose focus-visible:rounded-none focus-visible:border-b focus-visible:border-b-border focus-visible:ring-0 focus-visible:ring-offset-0"
          />
        </CardContent>
      </Card>

      <div className="flex justify-end">
        <Button onClick={handleContinue} disabled={saving || patch.isPending}>
          Continue
        </Button>
      </div>
    </div>
  );
}
