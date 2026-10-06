import { useState } from "react";
import type { FormEvent } from "react";
import { Link, Navigate } from "react-router-dom";
import { Cpu, Eye, EyeOff, PlayCircle } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useAuth } from "@/lib/auth";
import { errorMessage } from "@/lib/api";
import { cn } from "@/lib/cn";
import { usePageTitle } from "@/lib/hooks/usePageTitle";
import type { Role } from "@/lib/types";
import { Alert } from "@/ui/Alert";
import { Button } from "@/ui/Button";
import { Field, Input } from "@/ui/Input";
import { AuthLayout } from "./AuthLayout";

const roles: { value: Role; title: string; body: string; icon: LucideIcon }[] = [
  { value: "customer", title: "Run workloads", body: "Submit containers and pay for the compute they use.", icon: PlayCircle },
  { value: "provider", title: "Rent out my machine", body: "Offer spare CPU and RAM and get paid when it's used.", icon: Cpu },
];

const MIN_PASSWORD = 8;

export function Signup() {
  usePageTitle("Create account");
  const { me, signup } = useAuth();
  const [role, setRole] = useState<Role>("customer");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [show, setShow] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [touched, setTouched] = useState(false);

  if (me) return <Navigate to="/" replace />;

  const passwordError =
    touched && password.length > 0 && password.length < MIN_PASSWORD ? `Use at least ${MIN_PASSWORD} characters.` : null;

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await signup(role, username.trim(), password);
    } catch (err) {
      setError(errorMessage(err, "Couldn't create the account"));
      setBusy(false);
    }
  }

  return (
    <AuthLayout>
      <div className="mb-7">
        <h1 className="text-2xl font-semibold tracking-tight text-fg">Create your account</h1>
        <p className="mt-1.5 text-sm text-muted">Pick how you'll use LazyCake. You can't change it later.</p>
      </div>

      <form onSubmit={submit} className="space-y-5" noValidate>
        {error && <Alert tone="danger">{error}</Alert>}

        <fieldset>
          <legend className="sr-only">Account type</legend>
          <div className="grid gap-2.5 sm:grid-cols-2" role="radiogroup" aria-label="Account type">
            {roles.map((r) => {
              const active = role === r.value;
              return (
                <button
                  key={r.value}
                  type="button"
                  role="radio"
                  aria-checked={active}
                  onClick={() => setRole(r.value)}
                  className={cn(
                    "flex min-w-0 flex-col items-start gap-2 rounded-xl border p-3.5 text-left transition-[border-color,background-color,box-shadow]",
                    active ? "border-accent/60 bg-accent/[0.06] shadow-glow" : "border-border bg-surface hover:border-border-strong"
                  )}
                >
                  <r.icon className={cn("size-5", active ? "text-accent" : "text-muted")} aria-hidden />
                  <span className="text-sm font-semibold text-fg">{r.title}</span>
                  <span className="text-xs leading-5 text-muted">{r.body}</span>
                </button>
              );
            })}
          </div>
        </fieldset>

        <Field label="Username">
          <Input
            name="username"
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="pick-a-username"
          />
        </Field>
        <Field
          label="Password"
          error={passwordError}
          hint={`At least ${MIN_PASSWORD} characters. There is no password reset: if you lose it, you lose the account.`}
        >
          <Input
            name="new-password"
            type={show ? "text" : "password"}
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            onBlur={() => setTouched(true)}
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

        <Button type="submit" size="lg" className="w-full" loading={busy} disabled={!username || password.length < MIN_PASSWORD}>
          Create account
        </Button>
      </form>

      <p className="mt-8 text-center text-sm text-muted">
        Already have an account?{" "}
        <Link to="/login" className="font-medium text-accent hover:underline">
          Sign in
        </Link>
      </p>
    </AuthLayout>
  );
}
