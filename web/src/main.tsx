import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Routes, Route } from "react-router-dom";
import "./index.css";
import App from "./App.tsx";
import Dashboard from "./routes/Dashboard.tsx";
import Optimize from "./routes/Optimize.tsx";
import AiMode from "./routes/AiMode.tsx";
import Sessions from "./routes/Sessions.tsx";
import Waterfall from "./routes/Waterfall.tsx";
import Waste from "./routes/Waste.tsx";
import Tools from "./routes/Tools.tsx";
import Search from "./routes/Search.tsx";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<App />}>
          <Route index element={<Dashboard />} />
          <Route path="optimize" element={<Optimize />} />
          <Route path="ai-mode" element={<AiMode />} />
          <Route path="sessions" element={<Sessions />} />
          <Route path="waterfall" element={<Waterfall />} />
          <Route path="waste" element={<Waste />} />
          <Route path="tools" element={<Tools />} />
          <Route path="search" element={<Search />} />
        </Route>
      </Routes>
    </BrowserRouter>
  </StrictMode>,
);
