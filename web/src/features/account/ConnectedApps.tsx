import { useState } from "react";
import { Plug } from "lucide-react";
import { apiDelete, apiGet, errorMessage } from "@/lib/api";
import { useAsync } from "@/lib/hooks/useAsync";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import { relativeTime } from "@/lib/format";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Card, CardBody } from "@/ui/Card";
import { CodeBlock } from "@/ui/CodeBlock";
import { EmptyState, ErrorState } from "@/ui/EmptyState";
import { PageHeader } from "@/ui/PageHeader";
import { useToast } from "@/ui/Toast";

interface Grant {
  id: string;
  client_name: string;
  created_at_ms: number;
  last_used_at_ms?: number;
}

/** AI apps connected to this account (MCP). Disconnecting one ends its access at once. */
export function ConnectedApps() {
  usePageTitle("Connected apps");
  const toast = useToast();
  const grants = useAsync((s) => apiGet<Grant[]>("/api/portal/oauth/grants", s), []);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const url = `${window.location.origin}/mcp`;

  async function disconnect(g: Grant) {
    setBusy(g.id);
    setError(null);
    try {
      await apiDelete(`/api/portal/oauth/grants/${encodeURIComponent(g.id)}`);
      toast.success("App disconnected", g.client_name);
      grants.reload();
    } catch (err) {
      setError(errorMessage(err, "Couldn't disconnect the app"));
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader title="Connected apps" description="AI apps that can use LazyCake for you. Disconnect one and it loses access at once." />

      <Card>
        <CardBody className="space-y-3">
          <p className="text-sm font-medium text-fg">Connect an AI app</p>
          <p className="text-sm text-muted">
            Add this address as an MCP server in your AI app. It opens a page here to ask you to allow it; there is nothing to copy or
            paste.
          </p>
          <CodeBlock code={url} />
        </CardBody>
      </Card>

      {error && <Alert tone="danger">{error}</Alert>}
      <Card className="overflow-hidden">
        {grants.error && !grants.data ? (
          <ErrorState title="Couldn't load connected apps" message={grants.error} onRetry={grants.reload} />
        ) : grants.data && grants.data.length === 0 ? (
          <EmptyState icon={Plug} title="No apps connected" description="Apps you allow will be listed here." compact />
        ) : (
          <ul className="divide-y divide-border">
            {(grants.data ?? []).map((g) => (
              <li key={g.id} className="flex items-center justify-between gap-4 px-5 py-4">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-fg">{g.client_name}</p>
                  <p className="text-xs text-muted">
                    Connected {relativeTime(g.created_at_ms)} ·{" "}
                    {g.last_used_at_ms ? `last used ${relativeTime(g.last_used_at_ms)}` : "not used yet"}
                  </p>
                </div>
                <Button variant="danger" size="sm" loading={busy === g.id} disabled={busy !== null} onClick={() => disconnect(g)}>
                  Disconnect
                </Button>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}
