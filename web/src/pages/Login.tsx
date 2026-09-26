import { useState } from "react";
import type { FormEvent } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { Button, Field, FormError, TextInput } from "@shared/components/Form";
import { useAuth } from "@shared/auth";
import { AuthLayout } from "./AuthLayout";

// One login form for both portals - the account's own role decides where
// it lands (App.tsx routes on useAuth().me.role once the unified
// POST /api/portal/login response comes back), so there's no "which
// portal is this" question to answer here at all.
export function Login() {
  const { login, error } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setSubmitting(true);
    try {
      await login(username, password);
      const from = (location.state as { from?: Location })?.from;
      navigate(from?.pathname ?? "/", { replace: true });
    } catch {
      // surfaced via `error`
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <AuthLayout
      title="Sign in"
      subtitle="Whether you're lending compute or running tasks, one account gets you in."
      footer={
        <>
          New here? <Link to="/signup" className="text-accent">Create an account</Link>
        </>
      }
    >
      <form onSubmit={handleSubmit}>
        <FormError message={error} />
        <Field label="Username">
          <TextInput value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required autoFocus />
        </Field>
        <Field label="Password">
          <TextInput
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </Field>
        <Button type="submit" disabled={submitting} className="w-full">
          {submitting ? "Signing in…" : "Sign in"}
        </Button>
      </form>
    </AuthLayout>
  );
}
