import { useState } from "react";
import { Download } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";

import { useInstallPrompt } from "./useInstallPrompt";

// InstallCard renders only when the browser has fired beforeinstallprompt
// and the app isn't already installed — so it self-hides on iOS, on
// installed PWAs, and on browsers that don't support programmatic
// install. Mounted at the top of Settings → General.
export function InstallCard() {
  const { canInstall, install } = useInstallPrompt();
  const [pending, setPending] = useState(false);

  if (!canInstall) return null;

  const onClick = async () => {
    setPending(true);
    try {
      await install();
    } finally {
      setPending(false);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-serif">Install app</CardTitle>
        <CardDescription>
          Add Journaling Guru to your home screen for one-tap access. Push
          reminders are more reliable from the installed app.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Button onClick={onClick} disabled={pending}>
          <Download className="size-4" />
          {pending ? "Opening…" : "Install"}
        </Button>
      </CardContent>
    </Card>
  );
}
