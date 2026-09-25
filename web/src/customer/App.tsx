import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { CustomerAuthProvider, RequireAuth, useCustomerAuth } from "@shared/auth";
import { Login } from "./pages/Login";
import { Signup } from "./pages/Signup";
import { Dashboard } from "./pages/Dashboard";
import { SubmitTask } from "./pages/SubmitTask";
import { TaskDetail } from "./pages/TaskDetail";
import { TaskHistory } from "./pages/TaskHistory";
import { Gateways } from "./pages/Gateways";
import { Billing } from "./pages/Billing";
import { CustomerShell } from "./CustomerShell";

export function App() {
  return (
    <CustomerAuthProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/signup" element={<Signup />} />
          <Route path="/*" element={<Protected />} />
        </Routes>
      </BrowserRouter>
    </CustomerAuthProvider>
  );
}

function Protected() {
  const { me, loading } = useCustomerAuth();
  return (
    <RequireAuth loading={loading} authed={!!me}>
      <CustomerShell>
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/tasks/new" element={<SubmitTask />} />
          <Route path="/tasks" element={<TaskHistory />} />
          <Route path="/tasks/:id" element={<TaskDetail />} />
          <Route path="/gateways" element={<Gateways />} />
          <Route path="/billing" element={<Billing />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </CustomerShell>
    </RequireAuth>
  );
}
