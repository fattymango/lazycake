import { Link, useNavigate } from "react-router-dom";
import { AuthForm } from "@shared/components/AuthForm";
import { useProviderAuth } from "@shared/auth";

export function Signup() {
  const { signup, error } = useProviderAuth();
  const navigate = useNavigate();

  async function handleSubmit(username: string, password: string) {
    await signup(username, password);
    navigate("/", { replace: true });
  }

  return (
    <AuthForm
      mode="signup"
      title="LazyCake — Provider"
      onSubmit={handleSubmit}
      error={error}
      warning="No password reset, no email on file. Losing your password loses the account — write it down somewhere safe."
      footer={
        <>
          Already have an account? <Link to="/login" className="text-accent">Log in</Link>
        </>
      }
    />
  );
}
