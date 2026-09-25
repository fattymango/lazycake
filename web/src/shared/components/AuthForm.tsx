import { useState } from "react";
import type { FormEvent } from "react";
import { Button, Field, FormError, TextInput } from "./Form";

// Shared shape of task 7.7's signup/login pages - one form, swapped between
// "log in" and "sign up" copy, used by both portals against their own auth
// hook (useCustomerAuth / useProviderAuth).
export function AuthForm({
  mode,
  title,
  onSubmit,
  error,
  footer,
  warning,
}: {
  mode: "login" | "signup";
  title: string;
  onSubmit: (username: string, password: string) => Promise<void>;
  error: string | null;
  footer: React.ReactNode;
  warning?: React.ReactNode;
}) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    try {
      await onSubmit(username, password);
    } catch {
      // error surfaced via the `error` prop from the auth hook
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center p-6 bg-bg text-text">
      <div className="max-w-sm w-full">
        <h1 className="text-xl font-semibold mb-1">{title}</h1>
        <p className="text-muted text-sm mb-6">
          {mode === "login" ? "Sign in to continue." : "Choose a username and password."}
        </p>

        {warning && mode === "signup" && (
          <div className="text-warn text-xs border border-warn/40 rounded p-3 mb-4">{warning}</div>
        )}

        <form onSubmit={handleSubmit}>
          <FormError message={error} />
          <Field label="Username">
            <TextInput
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="username"
              required
              autoFocus
            />
          </Field>
          <Field label="Password">
            <TextInput
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete={mode === "login" ? "current-password" : "new-password"}
              minLength={mode === "signup" ? 8 : undefined}
              required
            />
          </Field>
          <Button type="submit" disabled={submitting} className="w-full">
            {submitting ? "working…" : mode === "login" ? "Log in" : "Sign up"}
          </Button>
        </form>

        <div className="mt-6 text-sm text-muted">{footer}</div>
      </div>
    </div>
  );
}
