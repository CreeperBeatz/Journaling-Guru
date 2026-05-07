import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

// ResourcesPage is a static, hand-curated list of crisis resources.
// Linked from CrisisCard and Settings. Never LLM-generated; never
// localized at runtime — the copy here is reviewed.
//
// Routed at `/resources` (see router.tsx). Accessible without auth so
// a user in distress can reach it from anywhere, including the
// CrisisCard inside the chat.
export function ResourcesPage() {
  return (
    <main className="mx-auto max-w-2xl space-y-6 px-4 py-10">
      <header className="space-y-2">
        <p className="text-xs uppercase tracking-wide text-muted-foreground">
          Resources
        </p>
        <h1 className="font-serif text-h1">If you need someone to talk to</h1>
        <p className="text-sm text-muted-foreground">
          JournAI isn&apos;t a substitute for a real person. If you&apos;re in crisis or
          need support, please reach out to one of the lines below — they exist
          for exactly this.
        </p>
      </header>

      <Card>
        <CardHeader>
          <CardTitle className="font-serif text-h3">United States</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3 text-sm leading-relaxed">
          <p>
            <strong>988 Suicide and Crisis Lifeline</strong> — call or text{" "}
            <a className="underline" href="tel:988">988</a>. Free, confidential, 24/7.
          </p>
          <p>
            <strong>Crisis Text Line</strong> — text <strong>HOME</strong> to{" "}
            <a className="underline" href="sms:741741?body=HOME">741741</a>.
          </p>
          <p>
            <strong>Veterans Crisis Line</strong> — dial{" "}
            <a className="underline" href="tel:988">988</a>, then press 1; or
            text <strong>838255</strong>.
          </p>
          <p>
            <strong>Trevor Project (LGBTQ+ youth)</strong> — call{" "}
            <a className="underline" href="tel:18664887386">1-866-488-7386</a>{" "}
            or text <strong>START</strong> to <strong>678-678</strong>.
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="font-serif text-h3">International</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3 text-sm leading-relaxed">
          <p>
            <strong>findahelpline.com</strong> —{" "}
            <a
              className="underline"
              href="https://findahelpline.com"
              target="_blank"
              rel="noopener noreferrer"
            >
              findahelpline.com
            </a>{" "}
            lists crisis lines by country, free and verified.
          </p>
          <p>
            <strong>UK &amp; Ireland: Samaritans</strong> — call{" "}
            <a className="underline" href="tel:116123">116 123</a>. Free, 24/7.
          </p>
          <p>
            <strong>EU emergency number</strong> —{" "}
            <a className="underline" href="tel:112">112</a>.
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="font-serif text-h3">Immediate danger</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3 text-sm leading-relaxed">
          <p>
            If you or someone else is in immediate danger, call your local
            emergency number now — <strong>911</strong> (US/Canada),{" "}
            <strong>999</strong> (UK), <strong>112</strong> (EU/most of the
            world).
          </p>
        </CardContent>
      </Card>
    </main>
  );
}
