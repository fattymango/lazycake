import { useState } from "react";
import type { FormEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Button, Field, FormError, TextInput } from "@shared/components/Form";
import { IconEarnings, IconPlay } from "@shared/components/Icon";
import { useAuth } from "@shared/auth";
import type { Role } from "@shared/types";
import { AuthLayout } from "./AuthLayout";

// The one place a role is ever chosen - it doesn't exist yet, so nothing
// but the person creating the account can decide it. Every later sign-in
// uses the one unified login form and never asks again.
export function Signup() {
  const { signup, error } = useAuth();
  const navigate = useNavigate();
  const [role, setRole] = useState<Role>("customer");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    try {
      await signup(role, username, password);
      navigate("/", { replace: true });
    } catch {
      // surfaced via `error`
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <AuthLayout
      title="Create an account"
      subtitle="Choose a username and password. No email, no reset - write it down."
      footer={
        <>
          Already have an account? <Link to="/login" className="text-accent">Sign in</Link>
        </>
      }
    >
      <form onSubmit={handleSubmit}>
        <FormError message={error} />

        <div className="mb-4">
          <div className="text-xs font-medium text-muted mb-1.5">I want to</div>
          <div className="grid grid-cols-2 gap-2">
            <RoleOption
              active={role === "customer"}
              onClick={() => setRole("customer")}
              icon={IconPlay}
              title="Run tasks"
              subtitle="Pay for compute"
            />
            <RoleOption
              active={role === "provider"}
              onClick={() => setRole("provider")}
              icon={IconEarnings}
              title="Lend compute"
              subtitle="Earn from it"
            />
          </div>
        </div>

        <Field label="Username">
          <TextInput value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required autoFocus />
        </Field>
        <Field label="Password" hint="At least 8 characters.">
          <TextInput
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="new-password"
            minLength={8}
            required
          />
        </Field>
        <Button type="submit" disabled={submitting} className="w-full">
          {submitting ? "Creating…" : "Create account"}
        </Button>
      </form>
    </AuthLayout>
  );
}

function RoleOption({
  active,
  onClick,
  icon: Icon,
  title,
  subtitle,
}: {
  active: boolean;
  onClick: () => void;
  icon: (props: { className?: string }) => React.ReactNode;
  title: string;
  subtitle: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`text-left rounded-lg border p-3 transition-colors ${
        active ? "border-accent bg-accent/10" : "border-border hover:border-accent/50"
      }`}
    >
      <Icon className={`w-4 h-4 mb-2 ${active ? "text-accent" : "text-muted"}`} />
      <div className={`text-sm font-medium ${active ? "text-accent" : "text-text"}`}>{title}</div>
      <div className="text-xs text-muted">{subtitle}</div>
    </button>
  );
}
