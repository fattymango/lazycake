import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { ProviderAuthProvider, RequireAuth, useProviderAuth } from "@shared/auth";
import { Login } from "./pages/Login";
import { Signup } from "./pages/Signup";
import { Dashboard } from "./pages/Dashboard";
import { AddMachine } from "./pages/AddMachine";
import { MachineDetail } from "./pages/MachineDetail";
import { Earnings } from "./pages/Earnings";
import { ProviderShell } from "./ProviderShell";

export function App() {
  return (
    <ProviderAuthProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/signup" element={<Signup />} />
          <Route path="/*" element={<Protected />} />
        </Routes>
      </BrowserRouter>
    </ProviderAuthProvider>
  );
}

function Protected() {
  const { me, loading } = useProviderAuth();
  return (
    <RequireAuth loading={loading} authed={!!me}>
      <ProviderShell>
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/machines/add" element={<AddMachine />} />
          <Route path="/machines/:id" element={<MachineDetail />} />
          <Route path="/earnings" element={<Earnings />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </ProviderShell>
    </RequireAuth>
  );
}
