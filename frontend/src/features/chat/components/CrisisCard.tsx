import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

interface Props {
  resourcesUrl: string;
  onResume: () => void;
}

// CrisisCard interrupts the message flow when the safety regex fires.
// Static, hand-curated copy — never LLM-generated. Inline (in the
// scroll list), not modal — interrupting with a Dialog is jarring.
//
// Two affordances: "Get support" (primary, links to the resources
// page) and "I want to keep talking" (secondary, dismisses and
// returns control to the composer). Either way the message stays in
// the transcript so context is preserved.
export function CrisisCard({ resourcesUrl, onResume }: Props) {
  return (
    <Card className="border-destructive/40 bg-destructive/5">
      <CardHeader>
        <CardTitle className="font-serif text-lg leading-snug text-foreground">
          Take care of yourself first
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm leading-relaxed text-foreground/85">
        <p>
          What you shared sounds heavy, and I&apos;m not the right place to carry that
          alone. If you&apos;re in the United States, you can reach the 988 Suicide
          and Crisis Lifeline by calling or texting <strong>988</strong>, or the
          Crisis Text Line by texting <strong>HOME</strong> to <strong>741741</strong>.
        </p>
        <p>
          Outside the US: see the resources page below for international lines.
        </p>
        <div className="flex flex-col gap-2 pt-1 sm:flex-row sm:items-center">
          <Button asChild>
            <a href={resourcesUrl} target="_blank" rel="noopener noreferrer">
              Get support
            </a>
          </Button>
          <Button variant="ghost" onClick={onResume}>
            I want to keep talking
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
