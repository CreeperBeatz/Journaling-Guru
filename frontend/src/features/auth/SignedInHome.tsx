import { useMutation } from "@tanstack/react-query";

import { ApiError } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

import { deleteAccount, logout } from "./api";
import { useInvalidateMe, useMe } from "./useAuth";

// SignedInHome is the post-login landing for Phase 2. Phases 3+ replace
// this with the daily-entry / history / summaries surfaces; for now it's
// just enough to verify the round trip and exercise logout / delete.
export function SignedInHome() {
  const me = useMe();
  const invalidateMe = useInvalidateMe();

  const signOut = useMutation<unknown, ApiError>({
    mutationFn: () => logout(),
    onSettled: () => invalidateMe(),
  });

  const deleteAcct = useMutation<unknown, ApiError>({
    mutationFn: () => deleteAccount(),
    onSettled: () => invalidateMe(),
  });

  const user = me.data;
  if (!user) return null; // AuthGate handles redirect

  return (
    <Card>
      <CardHeader>
        <CardTitle>Signed in</CardTitle>
        <CardDescription>
          You're signed in as <span className="font-medium">{user.email}</span>.
          Phase 3 will replace this stub with the daily-entry view.
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
        <Button
          variant="destructive"
          onClick={() => {
            if (confirm("Delete your account? This signs you out and clears your data.")) {
              deleteAcct.mutate();
            }
          }}
          disabled={deleteAcct.isPending}
        >
          {deleteAcct.isPending ? "Deleting…" : "Delete account"}
        </Button>
      </CardContent>
    </Card>
  );
}
