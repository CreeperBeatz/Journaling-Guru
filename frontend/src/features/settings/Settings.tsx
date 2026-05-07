import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";

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
import { QuestionEditor } from "@/features/journal/QuestionEditor";

import { updateMe, UpdateMePatch } from "./api";
import { AppearanceCard } from "./AppearanceCard";

const VALID_TABS = ["general", "notifications", "questions", "account"] as const;
type SettingsTab = (typeof VALID_TABS)[number];

const COMMON_TIMEZONES = [
  "UTC",
  "Europe/London",
  "Europe/Berlin",
  "Europe/Sofia",
  "America/New_York",
  "America/Chicago",
  "America/Denver",
  "America/Los_Angeles",
  "Asia/Tokyo",
  "Asia/Singapore",
  "Australia/Sydney",
];

function detectBrowserTimezone(): string | null {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || null;
  } catch {
    return null;
  }
}

function allBrowserTimezones(): string[] {
  const intl = Intl as typeof Intl & {
    supportedValuesOf?: (key: "timeZone") => string[];
  };
  if (typeof intl.supportedValuesOf === "function") {
    try {
      return intl.supportedValuesOf("timeZone").slice();
    } catch {
      /* fall through */
    }
  }
  return COMMON_TIMEZONES;
}

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
  const [reminderTime, setReminderTime] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [dayStart, setDayStart] = useState("06:00");

  useEffect(() => {
    if (me.data) {
      setTz(me.data.timezone);
      setReminderTime(me.data.reminder_time.slice(0, 5));
      setDisplayName(me.data.display_name ?? "");
      setDayStart(minutesToHHMM(me.data.day_start_minutes));
    }
  }, [me.data]);

  const tzOptions = useMemo(() => allBrowserTimezones(), []);
  const browserTz = detectBrowserTimezone();

  if (me.isPending) return <p className="text-sm text-muted-foreground">Loading…</p>;
  if (!me.data) return null;

  const dayStartMinutes = hhmmToMinutes(dayStart);
  const dirty =
    tz !== me.data.timezone ||
    reminderTime !== me.data.reminder_time.slice(0, 5) ||
    displayName !== (me.data.display_name ?? "") ||
    (dayStartMinutes !== null && dayStartMinutes !== me.data.day_start_minutes);

  const handleSave = () => {
    const patch: UpdateMePatch = {};
    if (tz !== me.data?.timezone) patch.timezone = tz;
    if (reminderTime !== me.data?.reminder_time.slice(0, 5)) patch.reminder_time = reminderTime;
    if (displayName !== (me.data?.display_name ?? "")) patch.display_name = displayName;
    if (
      dayStartMinutes !== null &&
      dayStartMinutes !== me.data?.day_start_minutes
    ) {
      patch.day_start_minutes = dayStartMinutes;
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
        <TabsList className="grid w-full grid-cols-4 sm:inline-flex sm:w-auto">
          <TabsTrigger value="general">General</TabsTrigger>
          <TabsTrigger value="questions">Questions</TabsTrigger>
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
                Determines what counts as "today" for your daily entry. Changing
                it can shift the current day forward or back by one.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
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
              {/* Gate on `tz` being non-empty so the button doesn't flash
                  during the initial render where me.data hasn't synced into
                  local state yet (tz === "" makes browserTz !== tz true
                  for one frame). */}
              {tz && browserTz && browserTz !== tz ? (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setTz(browserTz)}
                >
                  Use browser timezone ({browserTz})
                </Button>
              ) : null}
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
        </TabsContent>

        <TabsContent value="questions" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle className="font-serif">Daily questions</CardTitle>
              <CardDescription>
                Reorder, edit, and archive prompts — add new ones at the
                bottom. Archived questions keep their history but stop
                showing on Today.
              </CardDescription>
            </CardHeader>
            {/* p-0: let the QuestionEditor's row gutters and separators
                run edge-to-edge of the card, like a settings list. */}
            <CardContent className="p-0">
              <QuestionEditor />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="notifications" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle className="font-serif">Reminder time</CardTitle>
              <CardDescription>
                Local time of day for your daily reminder. Push notifications ship
                in Phase 5; this value is saved either way.
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

      {/* Sticky save bar. Mobile: parks flush on top of the bottom-tab bar
          (tab bar is h-14 + env(safe-area-inset-bottom)). Desktop: sticks at
          the bottom of the scroll container. Always rendered; disabled state
          carries "nothing to save" so it doesn't flicker on tab mount. */}
      <div className="sticky bottom-[calc(3.5rem+env(safe-area-inset-bottom))] z-10 -mx-4 flex justify-end border-t border-border bg-background/85 px-4 py-3 backdrop-blur-md md:bottom-0 md:-mx-8 md:px-8">
        <Button onClick={handleSave} disabled={!dirty || update.isPending}>
          {update.isPending ? "Saving…" : "Save changes"}
        </Button>
      </div>

      <p className="pt-2 text-xs text-muted-foreground">v2 · phase 4.1</p>
    </div>
  );
}
