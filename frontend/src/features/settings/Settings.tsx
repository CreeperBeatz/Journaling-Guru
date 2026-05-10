import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";

import { ApiError } from "@/api/client";
import { Button, buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { toast } from "@/components/ui/sonner";
import { cn } from "@/lib/utils";
import { ME_KEY, useInvalidateMe, useMe } from "@/features/auth/useAuth";
import { deleteAccount, logout, type User } from "@/features/auth/api";
import { hhmmToMinutes, minutesToHHMM } from "@/lib/dayStart";
import { allBrowserTimezones, detectBrowserTimezone } from "@/lib/timezone";
import { WEEKDAY_LABELS } from "@/lib/weekdays";
// QuestionEditor is retained in the codebase as scaffolding for future
// custom-prompts expansion (the questions table + handlers stay
// wired), but it's hidden from the user under the Energy Audit pivot —
// the five prompts are fixed, so a CRUD UI for them would be confusing.

import { RemindersCard } from "@/features/push/RemindersCard";

import { updateMe, UpdateMePatch } from "./api";
import { AppearanceCard } from "./AppearanceCard";

const VALID_TABS = ["general", "notifications", "account"] as const;

type SettingsTab = (typeof VALID_TABS)[number];

export function Settings() {
  const me = useMe();
  const qc = useQueryClient();
  const invalidateMe = useInvalidateMe();
  const [searchParams, setSearchParams] = useSearchParams();
  const requested = searchParams.get("tab") as SettingsTab | null;
  const tab: SettingsTab =
    requested && (VALID_TABS as readonly string[]).includes(requested)
      ? requested
      : "general";
  const setTab = (t: SettingsTab) => {
    const next = new URLSearchParams(searchParams);
    if (t === "general") next.delete("tab");
    else next.set("tab", t);
    setSearchParams(next, { replace: true });
  };

  const update = useMutation<User, ApiError, UpdateMePatch>({
    mutationFn: (patch) => updateMe(patch),
    onSuccess: (user) => {
      qc.setQueryData(ME_KEY, user);
      toast.success("Settings saved");
    },
    onError: (err) => {
      toast.error("Couldn't save settings", { description: err.message });
    },
  });

  const signOut = useMutation<unknown, ApiError>({
    mutationFn: () => logout(),
    onSettled: () => invalidateMe(),
  });

  const deleteAcct = useMutation<unknown, ApiError>({
    mutationFn: () => deleteAccount(),
    onSettled: () => invalidateMe(),
  });

  const [tz, setTz] = useState("");
  const [tzAuto, setTzAuto] = useState(true);
  const [reminderTime, setReminderTime] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [dayStart, setDayStart] = useState("06:00");
  const [reflectionWeekday, setReflectionWeekday] = useState<number>(0);

  useEffect(() => {
    if (me.data) {
      setTz(me.data.timezone);
      setTzAuto(me.data.timezone_auto);
      setReminderTime(me.data.reminder_time.slice(0, 5));
      setDisplayName(me.data.display_name ?? "");
      setDayStart(minutesToHHMM(me.data.day_start_minutes));
      setReflectionWeekday(me.data.reflection_weekday ?? 0);
    }
  }, [me.data]);

  const tzOptions = useMemo(() => allBrowserTimezones(), []);
  const browserTz = detectBrowserTimezone();

  if (me.isPending) return <p className="text-sm text-muted-foreground">Loading…</p>;
  if (!me.data) return null;

  const dayStartMinutes = hhmmToMinutes(dayStart);
  // In override mode the picker value matters; in auto mode it doesn't —
  // dirty only fires on the mode flip, since auto resyncs from the browser
  // hint on the next /api/me load.
  const tzDirty =
    tzAuto !== me.data.timezone_auto ||
    (!tzAuto && tz !== me.data.timezone);
  const dirty =
    tzDirty ||
    reminderTime !== me.data.reminder_time.slice(0, 5) ||
    displayName !== (me.data.display_name ?? "") ||
    (dayStartMinutes !== null && dayStartMinutes !== me.data.day_start_minutes) ||
    reflectionWeekday !== (me.data.reflection_weekday ?? 0);

  const handleSave = () => {
    const patch: UpdateMePatch = {};
    if (tzAuto !== me.data?.timezone_auto) patch.timezone_auto = tzAuto;
    if (!tzAuto && tz !== me.data?.timezone) patch.timezone = tz;
    if (reminderTime !== me.data?.reminder_time.slice(0, 5)) patch.reminder_time = reminderTime;
    if (displayName !== (me.data?.display_name ?? "")) patch.display_name = displayName;
    if (
      dayStartMinutes !== null &&
      dayStartMinutes !== me.data?.day_start_minutes
    ) {
      patch.day_start_minutes = dayStartMinutes;
    }
    if (reflectionWeekday !== (me.data?.reflection_weekday ?? 0)) {
      patch.reflection_weekday = reflectionWeekday;
    }
    if (Object.keys(patch).length === 0) return;
    update.mutate(patch);
  };

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <p className="text-xs uppercase tracking-wider text-muted-foreground">Settings</p>
        <h1 className="font-serif text-h1">Your account</h1>
        <p className="text-sm text-muted-foreground">{me.data.email}</p>
      </header>

      <Tabs value={tab} onValueChange={(v) => setTab(v as SettingsTab)}>
        <TabsList className="grid w-full grid-cols-3 sm:inline-flex sm:w-auto">
          <TabsTrigger value="general">General</TabsTrigger>
          <TabsTrigger value="notifications">Notifications</TabsTrigger>
          <TabsTrigger value="account">Account</TabsTrigger>
        </TabsList>

        <TabsContent value="general" className="space-y-4">
          <AppearanceCard />

          <Card>
            <CardHeader>
              <CardTitle className="font-serif">Profile</CardTitle>
              <CardDescription>Optional name shown in summaries (Phase 4).</CardDescription>
            </CardHeader>
            <CardContent className="space-y-2">
              <label htmlFor="display-name" className="block text-sm font-medium">Display name</label>
              <Input
                id="display-name"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                placeholder="e.g. Dani"
                maxLength={200}
              />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="font-serif">Timezone</CardTitle>
              <CardDescription>
                Determines what counts as "today" for your daily entry. By
                default the app follows your device. Pin a specific zone if
                you want to keep a fixed clock even when traveling.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div
                role="radiogroup"
                aria-label="Timezone mode"
                className="inline-flex rounded-md border border-border bg-muted p-1"
              >
                {[
                  { value: "auto", label: "Automatic" },
                  { value: "override", label: "Override" },
                ].map(({ value, label }) => {
                  const active = (value === "auto") === tzAuto;
                  return (
                    <button
                      key={value}
                      type="button"
                      role="radio"
                      aria-checked={active}
                      onClick={() => setTzAuto(value === "auto")}
                      className={cn(
                        "inline-flex h-8 items-center rounded px-3 text-sm font-medium transition-colors",
                        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ring-offset-background",
                        active
                          ? "bg-background text-foreground shadow-xs"
                          : "text-muted-foreground hover:text-foreground",
                      )}
                    >
                      {label}
                    </button>
                  );
                })}
              </div>

              {tzAuto ? (
                <div className="space-y-1">
                  <p className="text-sm">
                    Following your device · <span className="font-medium">{browserTz ?? me.data.timezone}</span>
                  </p>
                  <p className="text-xs text-muted-foreground">
                    The app picks up your browser's timezone every time it
                    loads.
                  </p>
                </div>
              ) : (
                <div className="space-y-2">
                  <label className="block text-sm font-medium">IANA timezone</label>
                  <Select value={tz} onValueChange={setTz}>
                    <SelectTrigger className="w-full">
                      <SelectValue placeholder="Select a timezone" />
                    </SelectTrigger>
                    <SelectContent className="max-h-72">
                      {tzOptions.map((z) => (
                        <SelectItem key={z} value={z}>
                          {z}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <p className="text-xs text-muted-foreground">
                    Pinned to <span className="font-medium">{tz || "—"}</span>. The app
                    will not follow your device.
                  </p>
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="font-serif">New day starts at</CardTitle>
              <CardDescription>
                The cutoff for "today". With 06:00, anything you write between
                00:00 and 05:59 still counts as the previous day — useful if you
                journal late at night before sleep.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-2">
              <label htmlFor="day-start" className="block text-sm font-medium">Cutoff</label>
              <Input
                id="day-start"
                type="time"
                value={dayStart}
                onChange={(e) => setDayStart(e.target.value)}
              />
              {dayStartMinutes === null ? (
                <p className="text-xs text-destructive">Use HH:MM format.</p>
              ) : null}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="font-serif">Weekly reflection day</CardTitle>
              <CardDescription>
                The day each week your check-in becomes a pattern view —
                top drainers, top chargers, and a chance to set a goal.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-2">
              <label htmlFor="reflection-day" className="block text-sm font-medium">
                Day
              </label>
              <Select
                value={String(reflectionWeekday)}
                onValueChange={(v) => setReflectionWeekday(Number(v))}
              >
                <SelectTrigger id="reflection-day" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {WEEKDAY_LABELS.map((label, idx) => (
                    <SelectItem key={idx} value={String(idx)}>
                      {label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </CardContent>
          </Card>

          {/* Low-emphasis link back into the first-run walkthrough.
              Useful for re-reading the intro, or for showing the app to
              someone else. ?replay=1 bypasses the "already onboarded →
              redirect" check inside OnboardingLayout. */}
          <p className="px-1 text-xs text-muted-foreground">
            <Link
              to="/onboarding?replay=1"
              className="underline-offset-2 hover:underline focus-visible:outline-none focus-visible:underline"
            >
              Replay walkthrough
            </Link>
            <span className="mx-1.5">·</span>
            Re-runs the first-time setup tour. Doesn't reset anything.
          </p>
        </TabsContent>

        <TabsContent value="notifications" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle className="font-serif">Reminder time</CardTitle>
              <CardDescription>
                Local time of day for the daily push reminder. Save the new
                time, then enable notifications below.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-2">
              <label htmlFor="reminder" className="block text-sm font-medium">Time</label>
              <Input
                id="reminder"
                type="time"
                value={reminderTime}
                onChange={(e) => setReminderTime(e.target.value)}
              />
            </CardContent>
          </Card>

          <RemindersCard />
        </TabsContent>

        <TabsContent value="account" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle className="font-serif">Sign out / delete account</CardTitle>
              <CardDescription>
                Sign out clears the cookie on this device. Delete account is a
                soft-delete: sessions are revoked, but the row is retained for a
                grace period before hard-delete (Phase 7).
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-wrap gap-3">
              <Button
                variant="secondary"
                onClick={() => signOut.mutate()}
                disabled={signOut.isPending}
              >
                {signOut.isPending ? "Signing out…" : "Sign out"}
              </Button>
              <AlertDialog>
                <AlertDialogTrigger asChild>
                  <Button variant="destructive" disabled={deleteAcct.isPending}>
                    {deleteAcct.isPending ? "Deleting…" : "Delete account"}
                  </Button>
                </AlertDialogTrigger>
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>Delete your account?</AlertDialogTitle>
                    <AlertDialogDescription>
                      This signs you out and clears your sessions. Your data is
                      retained for a grace period before hard-delete.
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <AlertDialogCancel>Cancel</AlertDialogCancel>
                    <AlertDialogAction
                      onClick={() => deleteAcct.mutate()}
                      className={cn(buttonVariants({ variant: "destructive" }))}
                    >
                      Delete account
                    </AlertDialogAction>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Sticky save bar. Sticks to the bottom of the scroll container on
          both mobile and desktop; mobile keeps clear of the safe-area inset.
          Always rendered; disabled state carries "nothing to save" so it
          doesn't flicker on tab mount. */}
      <div className="sticky bottom-[env(safe-area-inset-bottom)] z-10 -mx-4 flex justify-end border-t border-border bg-background/85 px-4 py-3 backdrop-blur-md md:bottom-0 md:-mx-8 md:px-8">
        <Button onClick={handleSave} disabled={!dirty || update.isPending}>
          {update.isPending ? "Saving…" : "Save changes"}
        </Button>
      </div>

      <p className="pt-2 text-xs text-muted-foreground">v2 · phase 5</p>
    </div>
  );
}
