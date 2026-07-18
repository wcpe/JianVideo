import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";

const root = document.getElementById("root");
if (!root) {
  throw new Error("未找到 mock-studio 挂载节点");
}

createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
