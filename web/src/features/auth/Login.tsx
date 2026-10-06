import { useState } from "react";
import type { FormEvent } from "react";
import { Link, Navigate, useLocation } from "react-router-dom";
import { Eye, EyeOff } from "lucide-react";
import { useAuth } from "@/lib/auth";
import { errorMessage } from "@/lib/api";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Field, Input } from "@/ui/Input";
import { AuthLayout } from "./AuthLayout";

export function Login() {
  usePageTitle("Sign in");
  const { me, login } = useAuth();
  const location = useLocation();
  const from = (location.state as { from?: string } | null)?.from ?? "/";
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [show, setShow] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  if (me) return <Navigate to={from} replace />;

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await login(username.trim(), password);
    } catch (err) {
      setError(errorMessage(err, "Couldn't sign in"));
      setBusy(false);
    }
  }

  return (
    <AuthLayout>
      <div className="mb-8">
        <h1 className="text-2xl font-semibold tracking-tight text-fg">Welcome back</h1>
        <p className="mt-1.5 text-sm text-muted">Sign in to your LazyCake account.</p>
      </div>

      <form onSubmit={submit} className="space-y-5" noValidate>
        {error && <Alert tone="danger">{error}</Alert>}
        <Field label="Username">
          <Input
            name="username"
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
            autoFocus
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="your-username"
          />
        </Field>
        <Field label="Password">
          <Input
            name="password"
            type={show ? "text" : "password"}
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            placeholder="Your password"
            trailing={
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                onClick={() => setShow((s) => !s)}
                aria-label={show ? "Hide password" : "Show password"}
              >
                {show ? <EyeOff /> : <Eye />}
              </Button>
            }
          />
        </Field>
        <Button type="submit" size="lg" className="w-full" loading={busy} disabled={!username || !password}>
          Sign in
        </Button>
      </form>

      <p className="mt-8 text-center text-sm text-muted">
        New to LazyCake?{" "}
        <Link to="/signup" className="font-medium text-accent hover:underline">
          Create an account
        </Link>
      </p>
    </AuthLayout>
  );
}
