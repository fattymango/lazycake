import { Link, useLocation, useNavigate } from "react-router-dom";
import { AuthForm } from "@shared/components/AuthForm";
import { useProviderAuth } from "@shared/auth";

export function Login() {
  const { login, error } = useProviderAuth();
  const navigate = useNavigate();
  const location = useLocation();

  async function handleSubmit(username: string, password: string) {
    await login(username, password);
    const from = (location.state as { from?: Location })?.from;
    navigate(from?.pathname ?? "/", { replace: true });
  }

  return (
    <AuthForm
      mode="login"
      title="LazyCake — Provider"
      onSubmit={handleSubmit}
      error={error}
      footer={
        <>
          New here? <Link to="/signup" className="text-accent">Create an account</Link>
        </>
      }
    />
  );
}
