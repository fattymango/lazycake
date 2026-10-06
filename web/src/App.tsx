import { BrowserRouter, Route, Routes } from "react-router-dom";
import { AuthProvider, useAuth } from "@/lib/auth";
import { ThemeProvider } from "@/lib/theme";
import { ErrorBoundary } from "@/ui/ErrorBoundary";
import { FullScreenError, FullScreenLoader } from "@/ui/FullScreenState";
import { ToastProvider } from "@/ui/Toast";
import { TooltipProvider } from "@/ui/Tooltip";
import { Login } from "@/features/auth/Login";
import { Signup } from "@/features/auth/Signup";
import { Kit } from "@/features/kit/Kit";
import { Portal } from "@/features/Portal";

// Providers outermost-first. The router sits inside auth so route guards can
// read the session, and the error boundary sits inside the router so a
// crash in one page doesn't take the navigation down with it (Portal re-keys
// it per route).
export function App() {
  return (
    <ThemeProvider>
      <TooltipProvider>
        <ToastProvider>
          <AuthProvider>
            <BrowserRouter>
              <Gate />
            </BrowserRouter>
          </AuthProvider>
        </ToastProvider>
      </TooltipProvider>
    </ThemeProvider>
  );
}

function Gate() {
  const { loading, serverError, retry } = useAuth();
  if (loading) return <FullScreenLoader />;
  if (serverError) {
    return <FullScreenError title="Can't reach LazyCake" message={serverError} onRetry={retry} />;
  }
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/signup" element={<Signup />} />
      {import.meta.env.DEV && <Route path="/_kit" element={<Kit />} />}
      <Route
        path="/*"
        element={
          <ErrorBoundary>
            <Portal />
          </ErrorBoundary>
        }
      />
    </Routes>
  );
}
