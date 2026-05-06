import { useEffect, useRef } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useMutation } from "@tanstack/react-query";

import { ApiError } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

import { verifyMagicLink } from "./api";
import { useInvalidateMe } from "./useAuth";

// MagicLinkVerify is the SPA route the email link opens. It POSTs the
// raw token to the backend exactly once on mount; success sets the
// session cookie via Set-Cookie and we redirect to the home page.
//
// React StrictMode mounts effects twice in dev, which would consume the
// single-use token before the user sees anything. The didFire ref guards
// against that without disabling StrictMode globally.
export function MagicLinkVerify() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const invalidateMe = useInvalidateMe();
  const didFire = useRef(false);

  const token = params.get("token") ?? "";

  const verify = useMutation<unknown, ApiError, string>({
    mutationFn: (t: string) => verifyMagicLink(t),
    onSuccess: async () => {
      await invalidateMe();
      navigate("/", { replace: true });
    },
  });

  useEffect(() => {
    if (didFire.current) return;
    didFire.current = true;
    if (token) verify.mutate(token);
  }, [token, verify]);

  if (!token) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Missing token</CardTitle>
          <CardDescription>
            This sign-in link is incomplete. Request a new one from the sign-in page.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button asChild>
            <Link to="/auth/login">Back to sign in</Link>
          </Button>
        </CardContent>
      </Card>
    );
  }

  if (verify.isError) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Link couldn't be used</CardTitle>
          <CardDescription>
            Magic links expire after 15 minutes and can only be used once. Request a new one.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button asChild>
            <Link to="/auth/login">Send a new link</Link>
          </Button>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Signing you in…</CardTitle>
        <CardDescription>One moment.</CardDescription>
      </CardHeader>
    </Card>
  );
}
