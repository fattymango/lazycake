import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { AuthProvider, RequireAuth, useAuth } from "@shared/auth";
import { Login } from "./pages/Login";
import { Signup } from "./pages/Signup";

import { CustomerShell } from "./customer/CustomerShell";
import { Dashboard as CustomerDashboard } from "./customer/pages/Dashboard";
import { SubmitTask } from "./customer/pages/SubmitTask";
import { TaskDetail } from "./customer/pages/TaskDetail";
import { TaskHistory } from "./customer/pages/TaskHistory";
import { Gateways } from "./customer/pages/Gateways";
import { Billing } from "./customer/pages/Billing";

import { ProviderShell } from "./provider/ProviderShell";
import { Dashboard as ProviderDashboard } from "./provider/pages/Dashboard";
import { AddMachine } from "./provider/pages/AddMachine";
import { MachineDetail } from "./provider/pages/MachineDetail";
import { Earnings } from "./provider/pages/Earnings";

// One app, one router, one session - which route tree mounts depends on
// the signed-in account's own role (useAuth().me.role, discovered from
// GET /api/portal/me), not on which HTML page happened to be loaded. This
// is what removes the "are you a customer or a provider" question on
// every visit: the account already knows, so the app just asks once,
// right after signing in, and never again for the life of the session.
export function App() {
  return (
    <AuthProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/signup" element={<Signup />} />
          <Route path="/*" element={<Protected />} />
        </Routes>
      </BrowserRouter>
    </AuthProvider>
  );
}

function Protected() {
  const { me, loading } = useAuth();
  return (
    <RequireAuth loading={loading} authed={!!me}>
      {me?.role === "provider" ? <ProviderRoutes /> : <CustomerRoutes />}
    </RequireAuth>
  );
}

function CustomerRoutes() {
  return (
    <CustomerShell>
      <Routes>
        <Route path="/" element={<CustomerDashboard />} />
        <Route path="/tasks/new" element={<SubmitTask />} />
        <Route path="/tasks" element={<TaskHistory />} />
        <Route path="/tasks/:id" element={<TaskDetail />} />
        <Route path="/gateways" element={<Gateways />} />
        <Route path="/billing" element={<Billing />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </CustomerShell>
  );
}

function ProviderRoutes() {
  return (
    <ProviderShell>
      <Routes>
        <Route path="/" element={<ProviderDashboard />} />
        <Route path="/machines/add" element={<AddMachine />} />
        <Route path="/machines/:id" element={<MachineDetail />} />
        <Route path="/earnings" element={<Earnings />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </ProviderShell>
  );
}
