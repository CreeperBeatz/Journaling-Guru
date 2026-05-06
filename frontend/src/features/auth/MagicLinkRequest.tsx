import { FormEvent, useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";

import { ApiError } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";

import { requestMagicLink } from "./api";
import { useRecentEmails } from "./recentEmails";

// MagicLinkRequest is the only sign-in/sign-up surface — passwordless, so
// the same form covers both. On success we don't redirect; we tell the
// user to check their inbox and stop.
//
// The most-recent address is prefilled on mount, and the full history is
// surfaced via a native <datalist> so the browser shows past emails as
// suggestions while typing.
export function MagicLinkRequest() {
  const [email, setEmail] = useState("");
  const [hydrated, setHydrated] = useState(false);
  const { recent, remember } = useRecentEmails();

  // Prefill once after the recent-emails hook has read from localStorage.
  // Guarded by `hydrated` so we don't clobber a value the user has already
  // started typing on a later remember() call.
  useEffect(() => {
    if (hydrated) return;
    if (recent.length > 0) setEmail(recent[0]);
    setHydrated(true);
  }, [recent, hydrated]);

  const send = useMutation<unknown, ApiError, string>({
    mutationFn: (e: string) => requestMagicLink(e),
    onSuccess: (_data, submittedEmail) => remember(submittedEmail),
  });

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    const cleaned = email.trim();
    if (!cleaned) return;
    send.mutate(cleaned);
  };

  if (send.isSuccess) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Check your inbox</CardTitle>
          <CardDescription>
            We sent a sign-in link to <span className="font-medium">{email}</span>. The link
            is good for 15 minutes and can only be used once.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button
            variant="ghost"
            onClick={() => {
              send.reset();
              setEmail("");
            }}
          >
            Use a different email
          </Button>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Sign in to JournAI</CardTitle>
        <CardDescription>
          Enter your email and we'll send you a one-time link.
          No passwords.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={onSubmit} className="space-y-3">
          <Input
            type="email"
            name="email"
            inputMode="email"
            autoComplete="email"
            list="journai-recent-emails"
            required
            placeholder="you@example.com"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            disabled={send.isPending}
          />
          {recent.length > 0 && (
            <datalist id="journai-recent-emails">
              {recent.map((e) => (
                <option key={e} value={e} />
              ))}
            </datalist>
          )}
          {send.isError && (
            <p className="text-sm text-destructive">
              {send.error.status === 429
                ? "Too many requests — wait a few minutes and try again."
                : send.error.message}
            </p>
          )}
          <Button type="submit" disabled={send.isPending} className="w-full">
            {send.isPending ? "Sending…" : "Email me a link"}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
