import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { apiGet, apiPost, ApiError } from "@shared/api";
import { copyToClipboard } from "@shared/clipboard";
import { Panel } from "@shared/components/Panel";
import { DataTable } from "@shared/components/DataTable";
import { ErrorState, LoadingState } from "@shared/components/States";
import { ConnectedBadge } from "@shared/components/StateBadge";
import { Button, Field, FormError, TextInput } from "@shared/components/Form";
import { dateTime } from "@shared/format";
import type { CreateGatewayRequest, CreateGatewayResponse, Gateway } from "@shared/types";

export function Gateways() {
  const [gateways, setGateways] = useState<Gateway[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [label, setLabel] = useState("");
  const [serviceName, setServiceName] = useState("");
  const [servicePort, setServicePort] = useState("");
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [justCreated, setJustCreated] = useState<CreateGatewayResponse | null>(null);
  const [copied, setCopied] = useState(false);

  async function load() {
    setError(null);
    try {
      setGateways(await apiGet<Gateway[]>("/api/portal/customer/gateways"));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to load gateways");
    }
  }

  useEffect(() => {
    load();
  }, []);

  async function handleCreate(e: FormEvent) {
    e.preventDefault();
    setFormError(null);
    setSubmitting(true);
    try {
      const body: CreateGatewayRequest = {
        label: label.trim(),
        services: serviceName.trim()
          ? [{ name: serviceName.trim(), port: parseInt(servicePort, 10) || 0 }]
          : [],
      };
      const created = await apiPost<CreateGatewayResponse>("/api/portal/customer/gateways", body);
      setJustCreated(created);
      setCopied(false);
      setLabel("");
      setServiceName("");
      setServicePort("");
      await load();
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : "failed to create gateway");
    } finally {
      setSubmitting(false);
    }
  }

  async function handleCopyToken() {
    if (!justCreated) return;
    const ok = await copyToClipboard(justCreated.install_token);
    if (ok) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } else {
      setFormError("couldn't copy automatically — select the token above and copy it manually");
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-xl font-semibold">Gateways</h1>

      <Panel title="Your gateways">
        {gateways === null && !error ? (
          <LoadingState />
        ) : error ? (
          <ErrorState message={error} onRetry={load} />
        ) : (
          <DataTable
            rows={gateways!}
            rowKey={(g) => g.id}
            emptyLabel="no gateways yet"
            columns={[
              { header: "label", render: (g) => g.label },
              { header: "id", render: (g) => <span className="font-mono text-xs text-muted">{g.id}</span> },
              { header: "status", render: (g) => <ConnectedBadge connected={g.connected} /> },
              {
                header: "services",
                render: (g) => (
                  <span className="font-mono text-xs text-muted">
                    {g.services.map((s) => `${s.name}:${s.port}`).join(", ") || "—"}
                  </span>
                ),
              },
              { header: "created", render: (g) => dateTime(g.created_at_ms) },
            ]}
          />
        )}
      </Panel>

      <Panel title="Create a gateway" className="max-w-md">
        <form onSubmit={handleCreate}>
          <FormError message={formError} />
          <Field label="Label">
            <TextInput value={label} onChange={(e) => setLabel(e.target.value)} required placeholder="home-lab" />
          </Field>
          <div className="grid grid-cols-2 gap-4">
            <Field label="Service name">
              <TextInput value={serviceName} onChange={(e) => setServiceName(e.target.value)} placeholder="db" />
            </Field>
            <Field label="Service port">
              <TextInput
                type="number"
                value={servicePort}
                onChange={(e) => setServicePort(e.target.value)}
                placeholder="5432"
              />
            </Field>
          </div>
          <Button type="submit" disabled={submitting}>
            {submitting ? "creating…" : "Create gateway"}
          </Button>
        </form>
      </Panel>

      {justCreated && (
        <Panel title={`Install token for "${justCreated.label}"`} className="max-w-md">
          <div className="flex flex-col gap-3">
            <div className="text-xs text-warn">
              This token will not be shown again. Copy it now and pass it as LAZYCAKE_TOKEN
              when starting the gateway process.
            </div>
            <pre className="bg-black/40 border border-border rounded p-3 font-mono text-xs overflow-x-auto whitespace-pre-wrap break-all">
              {justCreated.install_token}
            </pre>
            <div>
              <Button variant="secondary" onClick={handleCopyToken}>
                {copied ? "copied!" : "copy to clipboard"}
              </Button>
            </div>
          </div>
        </Panel>
      )}
    </div>
  );
}
