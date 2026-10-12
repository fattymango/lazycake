import { useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { Plug } from "lucide-react";
import { useAuth } from "@/lib/auth";
import { ApiError, apiGet, apiPost, errorMessage } from "@/lib/api";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Skeleton } from "@/ui/Skeleton";
import { AuthLayout } from "./AuthLayout";

const customerCan = [
  "Run containers on your account and spend from your balance",
  "See, read the output of, and stop your tasks",
  "See and create your gateways",
  "See your balance and charges",
];
const providerCan = ["See your machines, their tasks and your earnings", "Create install commands for new machines"];

/**
 * The page an AI app sends you to when it asks to connect (OAuth). You are already signed in (the route guard sends you to sign in first); this
 * only asks whether to let that app act for your account. The app never sees your password: it gets a token that you can revoke under
 * Connected apps.
 */
export function Authorize() {
  usePageTitle("Connect an app");
  const { me, logout } = useAuth();
  const location = useLocation();
  const navigate = useNavigate();
  const q = new URLSearchParams(location.search);
  const params = {
    client_id: q.get("client_id") ?? "",
    redirect_uri: q.get("redirect_uri") ?? "",
    code_challenge: q.get("code_challenge") ?? "",
    code_challenge_method: q.get("code_challenge_method") ?? "",
    state: q.get("state") ?? "",
  };
  const [name, setName] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<"allow" | "deny" | null>(null);

  useEffect(() => {
    const qs = new URLSearchParams({ client_id: params.client_id, redirect_uri: params.redirect_uri });
    apiGet<{ client_name: string }>(`/api/portal/oauth/client?${qs}`)
      .then((c) => setName(c.client_name))
      .catch((err) =>
        setError(err instanceof ApiError && err.status === 400 ? err.message : errorMessage(err, "Couldn't check this request"))
      );
    // The request is fixed for the life of the page.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function decide(approve: boolean) {
    setBusy(approve ? "allow" : "deny");
    setError(null);
    try {
      const r = await apiPost<{ redirect_to: string }>("/api/portal/oauth/authorize", { ...params, approve });
      window.location.assign(r.redirect_to); // back to the app (an https address, or the app's own scheme)
    } catch (err) {
      setError(errorMessage(err, "Couldn't complete the request"));
      setBusy(null);
    }
  }

  async function switchAccount() {
    await logout();
    navigate("/login", { state: { from: location.pathname + location.search } });
  }

  const can = me?.role === "provider" ? providerCan : customerCan;
  return (
    <AuthLayout>
      <div className="mb-6 flex items-center gap-3">
        <span className="flex size-10 items-center justify-center rounded-xl bg-accent/15 text-accent">
          <Plug className="size-5" aria-hidden />
        </span>
        <div>
          <h1 className="text-xl font-semibold tracking-tight text-fg">{name ? `Connect ${name}?` : "Connect an app"}</h1>
          <p className="text-sm text-muted">
            Signed in as <span className="font-medium text-fg">{me?.username}</span> ({me?.role}).{" "}
            <button type="button" onClick={switchAccount} className="text-accent underline-offset-2 hover:underline">
              Not you?
            </button>
          </p>
        </div>
      </div>

      {error && <Alert tone="danger">{error}</Alert>}
      {!error && name === null && <Skeleton className="h-40 w-full" />}

      {name !== null && !error && (
        <>
          <div className="rounded-xl border border-border bg-surface p-4">
            <p className="text-sm font-medium text-fg">{name} will be able to:</p>
            <ul className="mt-2 list-disc space-y-1 pl-5 text-sm text-muted">
              {can.map((c) => (
                <li key={c}>{c}</li>
              ))}
            </ul>
          </div>
          <p className="mt-3 text-xs text-muted">
            It acts as you, so what it runs is charged to your account. It never sees your password, and you can disconnect it any time
            under Connected apps.
          </p>
          <div className="mt-6 flex gap-3">
            <Button type="button" loading={busy === "allow"} disabled={busy !== null} onClick={() => decide(true)}>
              Allow
            </Button>
            <Button type="button" variant="secondary" loading={busy === "deny"} disabled={busy !== null} onClick={() => decide(false)}>
              Cancel
            </Button>
          </div>
        </>
      )}
    </AuthLayout>
  );
}
