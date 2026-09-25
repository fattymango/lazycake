import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { apiGet, apiPost, ApiError } from "@shared/api";
import { Panel } from "@shared/components/Panel";
import { Button, Field, FormError, TextArea, TextInput } from "@shared/components/Form";
import type { Gateway, SubmitTaskRequest, Task, TunnelTarget } from "@shared/types";

const defaultEnv = "";

export function SubmitTask() {
  const navigate = useNavigate();
  const [image, setImage] = useState("");
  const [cores, setCores] = useState("1");
  const [memoryMB, setMemoryMB] = useState("512");
  const [args, setArgs] = useState("");
  const [env, setEnv] = useState(defaultEnv);
  const [gatewayIDs, setGatewayIDs] = useState<string[]>([]);
  const [gateways, setGateways] = useState<Gateway[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    apiGet<Gateway[]>("/api/portal/customer/gateways")
      .then(setGateways)
      .catch(() => {});
  }, []);

  function toggleGateway(id: string) {
    setGatewayIDs((cur) => (cur.includes(id) ? cur.filter((g) => g !== id) : [...cur, id]));
  }

  function parseEnv(text: string): Record<string, string> {
    const out: Record<string, string> = {};
    for (const line of text.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      const idx = trimmed.indexOf("=");
      if (idx === -1) continue;
      out[trimmed.slice(0, idx)] = trimmed.slice(idx + 1);
    }
    return out;
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      const tunnelTargets: TunnelTarget[] = gatewayIDs.map((id) => {
        const gw = gateways.find((g) => g.id === id);
        const svc = gw?.services[0];
        return { gateway_id: id, hostname: svc?.name ?? "", port: svc?.port ?? 0 };
      });
      const body: SubmitTaskRequest = {
        image: image.trim(),
        args: args.trim() ? args.trim().split(/\s+/) : undefined,
        env: Object.keys(parseEnv(env)).length ? parseEnv(env) : undefined,
        cores: parseFloat(cores) || 1,
        memory_mb: parseInt(memoryMB, 10) || 512,
        tunnel_targets: tunnelTargets.length ? tunnelTargets : undefined,
      };
      const task = await apiPost<Task>("/api/portal/customer/tasks", body);
      navigate(`/tasks/${task.id}`);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "failed to submit task");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="max-w-2xl">
      <h1 className="text-xl font-semibold mb-6">Submit a task</h1>
      <Panel>
        <form onSubmit={handleSubmit}>
          <FormError message={error} />
          <Field label="Image">
            <TextInput
              value={image}
              onChange={(e) => setImage(e.target.value)}
              placeholder="docker.io/library/alpine:3.20"
              required
            />
          </Field>
          <Field label="Args (space-separated)">
            <TextInput value={args} onChange={(e) => setArgs(e.target.value)} placeholder="echo hello" />
          </Field>
          <div className="grid grid-cols-2 gap-4">
            <Field label="CPU cores">
              <TextInput type="number" min="0.1" step="0.1" value={cores} onChange={(e) => setCores(e.target.value)} required />
            </Field>
            <Field label="Memory (MB)">
              <TextInput type="number" min="16" step="16" value={memoryMB} onChange={(e) => setMemoryMB(e.target.value)} required />
            </Field>
          </div>
          <Field label="Env vars (KEY=value, one per line)">
            <TextArea rows={4} value={env} onChange={(e) => setEnv(e.target.value)} />
          </Field>
          {gateways.length > 0 && (
            <Field label="Gateways to tunnel through">
              <div className="flex flex-col gap-2">
                {gateways.map((gw) => (
                  <label key={gw.id} className="flex items-center gap-2 text-sm">
                    <input
                      type="checkbox"
                      checked={gatewayIDs.includes(gw.id)}
                      onChange={() => toggleGateway(gw.id)}
                    />
                    {gw.label}
                  </label>
                ))}
              </div>
            </Field>
          )}
          <Button type="submit" disabled={submitting}>
            {submitting ? "submitting…" : "Submit task"}
          </Button>
        </form>
      </Panel>
    </div>
  );
}
