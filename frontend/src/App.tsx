import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { AppLayout } from "@/components/layout/AppLayout";
import { DashboardPage } from "@/pages/DashboardPage";
import { ModelsPage } from "@/pages/ModelsPage";
import { ModelDetailPage } from "@/pages/ModelDetailPage";
import "./App.css";

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<AppLayout />}>
          <Route path="/" element={<Navigate to="/dashboard" replace />} />
          <Route path="/dashboard" element={<DashboardPage />} />
          <Route path="/models" element={<ModelsPage />} />
          <Route path="/models/:id" element={<ModelDetailPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}

export default App;

